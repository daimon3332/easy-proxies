package importer

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	defaultUINodePageSize = 100
	maxUINodePageSize     = 120
)

func (s *Service) UISummary() (UISummary, error) {
	summary := UISummary{UpdatedAt: time.Now()}
	for _, node := range s.store.ListNodes() {
		summary.Total++
		switch {
		case node.InPool || node.State == StateInPool:
			summary.InPool++
		case node.State == StateParsed:
			summary.Parsed++
		case node.State == StateTesting:
			summary.Testing++
		case node.State == StatePassed:
			summary.Passed++
		case node.State == StateFailed:
			summary.Failed++
		case node.State == StateExcluded:
			summary.Excluded++
		}
	}
	return summary, nil
}

func (s *Service) ListUINodes(query UINodeListQuery) (UINodeListResponse, error) {
	query, err := normalizeUINodeListQuery(query)
	if err != nil {
		return UINodeListResponse{}, err
	}

	nodes := s.syncRuntimeNodes(s.store.ListNodes())
	countries := make(map[string]struct{})
	tags := make(map[string]struct{})
	filtered := make([]ManagedNode, 0, len(nodes))
	for _, node := range nodes {
		if !matchesUIScope(node, query.Scope) {
			continue
		}
		if node.CountryCode != "" {
			countries[node.CountryCode] = struct{}{}
		}
		if tag := strings.TrimSpace(node.TagPrefix); tag != "" {
			tags[tag] = struct{}{}
		}
		if !matchesUIFilters(node, query) {
			continue
		}
		filtered = append(filtered, node)
	}

	sort.Slice(filtered, func(i, j int) bool {
		return compareUINodes(filtered[i], filtered[j], query.Sort, query.Order)
	})

	start := (query.Page - 1) * query.PageSize
	if start > len(filtered) {
		start = len(filtered)
	}
	end := min(start+query.PageSize, len(filtered))
	items := make([]UINodeListItem, 0, end-start)
	for _, node := range filtered[start:end] {
		items = append(items, uiNodeListItem(node))
	}

	return UINodeListResponse{
		Items:     items,
		Total:     len(filtered),
		Page:      query.Page,
		PageSize:  query.PageSize,
		Countries: sortedUIValues(countries),
		Tags:      sortedUIValues(tags),
		UpdatedAt: time.Now(),
	}, nil
}

func (s *Service) UIPortPreview(limit int) ([]UIPortPreviewItem, error) {
	if limit <= 0 {
		limit = 80
	}
	if limit > 200 {
		limit = 200
	}

	nodes := s.syncRuntimeNodes(s.store.ListPoolNodes())
	if len(nodes) > limit {
		nodes = nodes[:limit]
	}
	items := make([]UIPortPreviewItem, 0, len(nodes))
	for _, node := range nodes {
		items = append(items, UIPortPreviewItem{
			Name:        node.Name,
			TagPrefix:   node.TagPrefix,
			CountryCode: node.CountryCode,
			Port:        node.Port,
		})
	}
	return items, nil
}

func (s *Service) ReorderPoolBy(mode string) error {
	mode = strings.TrimSpace(mode)
	if mode != "country" && mode != "tag" && mode != "latency" {
		return fmt.Errorf("不支持的节点池排序方式 %q", mode)
	}
	nodes := s.syncRuntimeNodes(s.store.ListPoolNodes())
	sort.Slice(nodes, func(i, j int) bool {
		return compareUINodes(nodes[i], nodes[j], mode, "asc")
	})
	ids := make([]string, 0, len(nodes))
	for _, node := range nodes {
		ids = append(ids, node.ID)
	}
	return s.Reorder(ids)
}

func (s *Service) resolveBatchTestNodeIDs(req BatchTestRequest) ([]string, error) {
	if len(req.NodeIDs) > 0 {
		return uniqueNodeIDs(req.NodeIDs), nil
	}
	if len(req.Scopes) == 0 {
		return nil, fmt.Errorf("请选择节点")
	}

	scopes := make(map[string]struct{}, len(req.Scopes))
	for _, scope := range req.Scopes {
		scope = strings.TrimSpace(scope)
		switch scope {
		case "candidate", "pool", "failed":
			scopes[scope] = struct{}{}
		default:
			return nil, fmt.Errorf("不支持的测试范围 %q", scope)
		}
	}

	ids := make([]string, 0)
	for _, node := range s.store.ListNodes() {
		if _, ok := scopes["candidate"]; ok && node.State == StatePassed && !node.InPool {
			ids = append(ids, node.ID)
			continue
		}
		if _, ok := scopes["pool"]; ok && (node.InPool || node.State == StateInPool) {
			ids = append(ids, node.ID)
			continue
		}
		if _, ok := scopes["failed"]; ok && node.State == StateFailed {
			ids = append(ids, node.ID)
		}
	}
	return ids, nil
}

func normalizeUINodeListQuery(query UINodeListQuery) (UINodeListQuery, error) {
	query.Scope = strings.TrimSpace(query.Scope)
	switch query.Scope {
	case "candidate", "pool", "failed", "all":
	default:
		return UINodeListQuery{}, fmt.Errorf("不支持的节点范围 %q", query.Scope)
	}
	query.Country = strings.TrimSpace(query.Country)
	query.Tag = strings.TrimSpace(query.Tag)
	query.Query = strings.ToLower(strings.TrimSpace(query.Query))
	query.Latency = strings.TrimSpace(query.Latency)
	query.Sort = strings.TrimSpace(query.Sort)
	if query.Sort == "" {
		query.Sort = "latency"
	}
	switch query.Sort {
	case "name", "latency", "country", "port", "tag":
	default:
		return UINodeListQuery{}, fmt.Errorf("不支持的排序字段 %q", query.Sort)
	}
	query.Order = strings.TrimSpace(query.Order)
	if query.Order == "" {
		query.Order = "asc"
	}
	if query.Order != "asc" && query.Order != "desc" {
		return UINodeListQuery{}, fmt.Errorf("不支持的排序方向 %q", query.Order)
	}
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 {
		query.PageSize = defaultUINodePageSize
	}
	if query.PageSize > maxUINodePageSize {
		query.PageSize = maxUINodePageSize
	}
	return query, nil
}

func matchesUIScope(node ManagedNode, scope string) bool {
	switch scope {
	case "candidate":
		return node.State == StatePassed && !node.InPool
	case "pool":
		return node.InPool || node.State == StateInPool
	case "failed":
		return node.State == StateFailed
	default:
		return true
	}
}

func matchesUIFilters(node ManagedNode, query UINodeListQuery) bool {
	if query.Country != "" && node.CountryCode != query.Country {
		return false
	}
	if query.Tag != "" && node.TagPrefix != query.Tag {
		return false
	}
	if !matchesUILatency(node.LatencyMs, query.Latency) {
		return false
	}
	if query.Query == "" {
		return true
	}
	search := strings.ToLower(strings.Join([]string{
		node.Name,
		node.OriginalName,
		node.TagPrefix,
		node.CountryCode,
		node.CountryName,
		node.ImportSource,
		node.ImportFormat,
		string(node.State),
	}, " "))
	return strings.Contains(search, query.Query)
}

func matchesUILatency(latency int64, filter string) bool {
	switch filter {
	case "", "all":
		return true
	case "none":
		return latency <= 0
	case "0-500":
		return latency > 0 && latency <= 500
	case "500-1500":
		return latency > 500 && latency <= 1500
	case "1500+":
		return latency > 1500
	default:
		return false
	}
}

func compareUINodes(a, b ManagedNode, field, order string) bool {
	if emptyUIValue(a, field) != emptyUIValue(b, field) {
		return !emptyUIValue(a, field)
	}

	direction := 1
	if order == "desc" {
		direction = -1
	}
	compare := 0
	switch field {
	case "latency":
		compare = compareUIInts(a.LatencyMs, b.LatencyMs)
	case "country":
		compare = strings.Compare(a.CountryCode, b.CountryCode)
	case "port":
		compare = compareUIInts(int64(a.Port), int64(b.Port))
	case "tag":
		compare = strings.Compare(a.TagPrefix, b.TagPrefix)
	default:
		compare = strings.Compare(a.Name, b.Name)
	}
	if compare == 0 {
		compare = strings.Compare(a.Name, b.Name)
	}
	if compare == 0 {
		compare = strings.Compare(a.ID, b.ID)
	}
	return compare*direction < 0
}

func emptyUIValue(node ManagedNode, field string) bool {
	switch field {
	case "latency":
		return node.LatencyMs <= 0
	case "country":
		return node.CountryCode == ""
	case "port":
		return node.Port == 0
	case "tag":
		return node.TagPrefix == ""
	default:
		return node.Name == ""
	}
}

func uiNodeListItem(node ManagedNode) UINodeListItem {
	return UINodeListItem{
		ID:           node.ID,
		OriginalName: truncateUIString(node.OriginalName, 160),
		Name:         truncateUIString(node.Name, 160),
		TagPrefix:    truncateUIString(node.TagPrefix, 80),
		CountryCode:  truncateUIString(node.CountryCode, 16),
		CountryName:  truncateUIString(node.CountryName, 80),
		LatencyMs:    node.LatencyMs,
		Port:         node.Port,
		State:        node.State,
		InPool:       node.InPool,
		Order:        node.Order,
		LastError:    truncateUIString(node.LastError, 512),
	}
}

func truncateUIString(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}

func sortedUIValues(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func uniqueNodeIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func compareUIInts(a, b int64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
