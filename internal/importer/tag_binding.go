package importer

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

type tagBindingStage struct {
	ImportID string
	Revision uint64
	NodeIDs  []string
	Noop     bool
}

type tagBindingCandidate struct {
	template ManagedNode
	refs     []NodeSourceRef
}

func (s *Service) StartTagBinding(req TagBindingRequest) (string, error) {
	s.tagBindingStartMu.Lock()
	defer s.tagBindingStartMu.Unlock()
	s.tagBindingJobsMu.RLock()
	for _, job := range s.tagBindingJobs {
		if job != nil && job.Status == "running" {
			s.tagBindingJobsMu.RUnlock()
			return "", fmt.Errorf("已有前置代理绑定任务正在运行")
		}
	}
	s.tagBindingJobsMu.RUnlock()
	if s.HasActiveJobs() {
		return "", fmt.Errorf("已有节点测试或刷新任务正在运行，请等待任务结束")
	}

	policy, err := NormalizeVerificationPolicy(req.Test204, req.SiteTargets)
	if err != nil {
		return "", err
	}
	req.ChainProfileID = strings.TrimSpace(req.ChainProfileID)
	if req.ChainProfileID != "" {
		if s.tester == nil {
			return "", fmt.Errorf("节点测试器不可用")
		}
		profile, ok := s.tester.ChainProfile(req.ChainProfileID)
		if !ok || !profile.Enabled {
			return "", fmt.Errorf("选择的前置代理不存在或未启用")
		}
	}
	tags := normalizeBindingTags(req.Tags)
	if len(tags) == 0 {
		return "", fmt.Errorf("请至少选择一个 Tag")
	}
	available := make(map[string]struct{})
	for _, node := range s.store.ListNodes() {
		if node.ImportMode == "import_stage" || node.ImportMode == "refresh_stage" {
			continue
		}
		for _, ref := range nodeSourceRefs(node) {
			if tag := strings.TrimSpace(ref.TagPrefix); tag != "" {
				available[tag] = struct{}{}
			}
		}
	}
	for _, tag := range tags {
		if _, ok := available[tag]; !ok {
			return "", fmt.Errorf("Tag %q 不存在或没有可绑定节点", tag)
		}
	}

	now := time.Now()
	jobID := randomHex(12)
	items := make([]TagBindingItem, len(tags))
	for index, tag := range tags {
		items[index] = TagBindingItem{TagPrefix: tag, Status: "waiting", UpdatedAt: now}
	}
	job := &TagBindingJob{
		ID: jobID, Status: "running", ChainProfileID: req.ChainProfileID,
		Test204: policy.Test204, SiteTargets: append([]string(nil), policy.SiteTargets...),
		TotalTags: len(tags), Items: items, CreatedAt: now, UpdatedAt: now,
	}
	s.tagBindingJobsMu.Lock()
	s.tagBindingJobs[jobID] = job
	s.tagBindingJobsMu.Unlock()
	started := s.launchBackground(func(cancel context.CancelFunc) {
		s.tagBindingCancelsMu.Lock()
		s.tagBindingCancels[jobID] = cancel
		s.tagBindingCancelsMu.Unlock()
	}, func(ctx context.Context) {
		s.runTagBindingJob(ctx, jobID, policy)
	})
	if !started {
		s.updateTagBindingJob(jobID, func(current *TagBindingJob) {
			current.Status = "failed"
			current.Error = "服务正在关闭"
		})
		return "", fmt.Errorf("服务正在关闭")
	}
	return jobID, nil
}

func normalizeBindingTags(tags []string) []string {
	result := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, raw := range tags {
		tag := strings.TrimSpace(raw)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		result = append(result, tag)
	}
	sort.Strings(result)
	return result
}

func (s *Service) runTagBindingJob(ctx context.Context, jobID string, policy VerificationPolicy) {
	defer func() {
		s.tagBindingCancelsMu.Lock()
		delete(s.tagBindingCancels, jobID)
		s.tagBindingCancelsMu.Unlock()
	}()
	job, ok := s.GetTagBindingJob(jobID)
	if !ok {
		return
	}
	for index := range job.Items {
		if ctx.Err() != nil {
			s.finishCanceledTagBindingJob(jobID, index)
			return
		}
		tag := job.Items[index].TagPrefix
		s.updateTagBindingItem(jobID, index, func(item *TagBindingItem) {
			item.Status = "preparing"
			item.Error = ""
		})
		stage, err := s.stageTagBinding(tag, job.ChainProfileID)
		if err != nil {
			s.finishTagBindingItem(jobID, index, "failed", ImportJob{}, err)
			continue
		}
		if stage.Noop {
			s.finishTagBindingItem(jobID, index, "skipped", ImportJob{}, nil)
			continue
		}
		test204, sites := VerificationPolicyPointers(policy)
		commit, err := s.Commit(stage.ImportID, CommitRequest{
			NodeIDs: stage.NodeIDs, AutoReload: true, PromotePassed: true,
			Test204: test204, SiteTargets: sites,
		})
		if err != nil {
			_ = s.cleanupStagedNodes(stage.NodeIDs)
			s.finishTagBindingItem(jobID, index, "failed", ImportJob{}, err)
			continue
		}
		s.updateTagBindingItem(jobID, index, func(item *TagBindingItem) {
			item.Status = "testing"
			item.JobID = commit.JobID
		})
		child, waitErr := s.waitImportJob(ctx, commit.JobID, func(progress ImportJob) {
			s.updateTagBindingItem(jobID, index, func(item *TagBindingItem) {
				item.Total = progress.Total
				item.Passed = progress.Passed
				item.Failed = progress.Failed
				item.Promoted = progress.Promoted
			})
		})
		if waitErr != nil || child.Status == ImportStatusCanceled {
			_ = s.cleanupStagedNodes(stage.NodeIDs)
			s.finishTagBindingItem(jobID, index, "canceled", child, waitErr)
			if ctx.Err() != nil {
				s.finishCanceledTagBindingJob(jobID, index+1)
				return
			}
			continue
		}
		if child.Status == ImportStatusFailed || child.Passed == 0 {
			_ = s.cleanupStagedNodes(stage.NodeIDs)
			message := strings.TrimSpace(child.Error)
			if message == "" {
				message = "新链路没有可用节点，已保留原绑定"
			}
			s.finishTagBindingItem(jobID, index, "failed", child, fmt.Errorf("%s", message))
			continue
		}
		s.finishTagBindingItem(jobID, index, "completed", child, nil)
	}
	s.updateTagBindingJob(jobID, func(current *TagBindingJob) {
		summarizeTagBindingJob(current)
	})
}

func (s *Service) stageTagBinding(tagPrefix, chainProfileID string) (tagBindingStage, error) {
	tagPrefix = strings.TrimSpace(tagPrefix)
	chainProfileID = strings.TrimSpace(chainProfileID)
	if tagPrefix == "" {
		return tagBindingStage{}, fmt.Errorf("Tag 不能为空")
	}
	if chainProfileID != "" {
		if s.tester == nil {
			return tagBindingStage{}, fmt.Errorf("节点测试器不可用")
		}
		profile, ok := s.tester.ChainProfile(chainProfileID)
		if !ok || !profile.Enabled {
			return tagBindingStage{}, fmt.Errorf("选择的前置代理不存在或未启用")
		}
	}
	candidates := make(map[string]*tagBindingCandidate)
	found := false
	changed := false
	for _, node := range s.store.ListNodes() {
		if node.ImportMode == "import_stage" || node.ImportMode == "refresh_stage" {
			continue
		}
		selected := make([]NodeSourceRef, 0)
		for _, ref := range nodeSourceRefs(node) {
			if strings.TrimSpace(ref.TagPrefix) != tagPrefix {
				continue
			}
			found = true
			if strings.TrimSpace(ref.ChainProfileID) != chainProfileID {
				changed = true
			}
			ref.ChainProfileID = chainProfileID
			selected = append(selected, ref)
		}
		if len(selected) == 0 || strings.TrimSpace(node.URI) == "" {
			continue
		}
		candidate := candidates[node.URI]
		if candidate == nil {
			candidate = &tagBindingCandidate{template: node}
			candidates[node.URI] = candidate
		} else if (node.InPool || node.State == StateInPool) && !(candidate.template.InPool || candidate.template.State == StateInPool) {
			candidate.template = node
		}
		candidate.refs = deduplicateSourceRefs(append(candidate.refs, selected...))
	}
	if !found || len(candidates) == 0 {
		return tagBindingStage{}, fmt.Errorf("Tag %q 没有可绑定节点", tagPrefix)
	}
	if !changed {
		return tagBindingStage{Noop: true}, nil
	}

	importID := randomHex(12)
	revision := s.nextSourceRevision(tagPrefix)
	now := time.Now()
	nodes := make([]ManagedNode, 0, len(candidates))
	for _, candidate := range candidates {
		node := candidate.template
		node.ID = s.routeNodeID(node.URI, chainProfileID) + "-import-" + importID
		node.ChainProfileID = chainProfileID
		node.SourceRefs = candidate.refs
		applyPrimarySource(&node, candidate.refs[0])
		node.ImportID = importID
		node.ImportMode = "import_stage"
		node.TagPrefix = tagPrefix
		node.ChainProfileID = chainProfileID
		node.Name = taggedOriginalName(tagPrefix, node.OriginalName)
		node.State = StateParsed
		node.Enabled = true
		node.InPool = false
		node.Port = 0
		node.ConsecutiveFailures = 0
		node.LastError = ""
		node.LastTestAt = time.Time{}
		node.CreatedAt = now
		node.UpdatedAt = now
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	nodeIDs := make([]string, len(nodes))
	for index := range nodes {
		nodeIDs[index] = nodes[index].ID
	}
	if err := s.store.UpsertNodes(nodes); err != nil {
		return tagBindingStage{}, fmt.Errorf("准备绑定节点: %w", err)
	}
	job := ImportJob{
		ID: importID, Status: ImportStatusParsed, Mode: "binding", Format: "tag_binding",
		TagPrefix: tagPrefix, Source: "tag:" + tagPrefix, SourceRevision: revision,
		ChainProfileID: chainProfileID, Total: len(nodes), NodeIDs: nodeIDs,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.store.UpsertJob(job); err != nil {
		_ = s.cleanupStagedNodes(nodeIDs)
		return tagBindingStage{}, fmt.Errorf("保存绑定任务: %w", err)
	}
	return tagBindingStage{ImportID: importID, Revision: revision, NodeIDs: nodeIDs}, nil
}

func (s *Service) GetTagBindingJob(jobID string) (TagBindingJob, bool) {
	s.tagBindingJobsMu.RLock()
	defer s.tagBindingJobsMu.RUnlock()
	job, ok := s.tagBindingJobs[strings.TrimSpace(jobID)]
	if !ok || job == nil {
		return TagBindingJob{}, false
	}
	return cloneTagBindingJob(job), true
}

func (s *Service) CancelTagBindingJob(jobID string) (TagBindingJob, error) {
	job, ok := s.GetTagBindingJob(jobID)
	if !ok {
		return TagBindingJob{}, fmt.Errorf("前置代理绑定任务不存在")
	}
	if job.Status != "running" {
		return job, nil
	}
	s.tagBindingCancelsMu.Lock()
	cancel := s.tagBindingCancels[jobID]
	s.tagBindingCancelsMu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.updateTagBindingJob(jobID, func(current *TagBindingJob) {
		current.Error = "正在终止"
	})
	job, _ = s.GetTagBindingJob(jobID)
	return job, nil
}

func (s *Service) updateTagBindingItem(jobID string, index int, update func(*TagBindingItem)) {
	s.updateTagBindingJob(jobID, func(job *TagBindingJob) {
		if index < 0 || index >= len(job.Items) {
			return
		}
		update(&job.Items[index])
		job.Items[index].UpdatedAt = time.Now()
	})
}

func (s *Service) finishTagBindingItem(jobID string, index int, status string, child ImportJob, err error) {
	s.updateTagBindingJob(jobID, func(job *TagBindingJob) {
		if index < 0 || index >= len(job.Items) {
			return
		}
		item := &job.Items[index]
		item.Status = status
		item.Total = child.Total
		item.Passed = child.Passed
		item.Failed = child.Failed
		item.Promoted = child.Promoted
		if err != nil {
			item.Error = err.Error()
		}
		item.UpdatedAt = time.Now()
		summarizeTagBindingJob(job)
	})
}

func (s *Service) finishCanceledTagBindingJob(jobID string, start int) {
	s.updateTagBindingJob(jobID, func(job *TagBindingJob) {
		for index := start; index < len(job.Items); index++ {
			if job.Items[index].Status == "waiting" || job.Items[index].Status == "preparing" || job.Items[index].Status == "testing" {
				job.Items[index].Status = "canceled"
				job.Items[index].Error = "任务已取消"
				job.Items[index].UpdatedAt = time.Now()
			}
		}
		job.Status = "canceled"
		job.Error = "任务已取消"
		summarizeTagBindingJob(job)
		job.Status = "canceled"
	})
}

func (s *Service) updateTagBindingJob(jobID string, update func(*TagBindingJob)) {
	s.tagBindingJobsMu.Lock()
	defer s.tagBindingJobsMu.Unlock()
	job := s.tagBindingJobs[jobID]
	if job == nil {
		return
	}
	update(job)
	job.UpdatedAt = time.Now()
}

func summarizeTagBindingJob(job *TagBindingJob) {
	job.DoneTags = 0
	job.Successful = 0
	job.Failed = 0
	job.Skipped = 0
	for _, item := range job.Items {
		switch item.Status {
		case "completed":
			job.DoneTags++
			job.Successful++
		case "failed":
			job.DoneTags++
			job.Failed++
		case "skipped":
			job.DoneTags++
			job.Skipped++
		case "canceled":
			job.DoneTags++
			job.Failed++
		}
	}
	if job.DoneTags < job.TotalTags {
		return
	}
	switch {
	case job.Failed == 0:
		job.Status = "completed"
		job.Error = ""
	case job.Successful+job.Skipped > 0:
		job.Status = "partial"
		job.Error = fmt.Sprintf("%d 个 Tag 修改失败并保留原绑定", job.Failed)
	default:
		job.Status = "failed"
		job.Error = "所有 Tag 修改失败，原绑定已保留"
	}
}

func cloneTagBindingJob(job *TagBindingJob) TagBindingJob {
	copyJob := *job
	copyJob.SiteTargets = append([]string(nil), job.SiteTargets...)
	copyJob.Items = append([]TagBindingItem(nil), job.Items...)
	return copyJob
}
