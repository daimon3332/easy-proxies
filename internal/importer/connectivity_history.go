package importer

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const connectivityHistoryRunsPerScope = 20

type storedConnectivityRun struct {
	ID          string
	CompletedAt time.Time
	Results     map[string]ConnectivityResult
}

func connectivityScopeKey(tags, targets []string) string {
	payload, _ := json.Marshal([][]string{append([]string(nil), tags...), append([]string(nil), targets...)})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:16])
}

func (s *stateDB) saveConnectivityRun(job ConnectivityJob, results map[string]ConnectivityResult) error {
	tagsJSON, err := json.Marshal(job.Tags)
	if err != nil {
		return err
	}
	targetsJSON, err := json.Marshal(job.Targets)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO connectivity_runs(id, scope_key, tags_json, targets_json, completed_at)
		VALUES(?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET scope_key=excluded.scope_key,
		tags_json=excluded.tags_json, targets_json=excluded.targets_json, completed_at=excluded.completed_at`,
		job.ID, connectivityScopeKey(job.Tags, job.Targets), string(tagsJSON), string(targetsJSON), job.UpdatedAt.UnixNano()); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM connectivity_results WHERE run_id = ?`, job.ID); err != nil {
		return err
	}
	statement, err := tx.Prepare(`INSERT INTO connectivity_results(
		run_id, route_fingerprint, node_name, tags_json, target_id, verdict, success, latency_ms, tested_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	for _, result := range results {
		resultTags, marshalErr := json.Marshal(result.Tags)
		if marshalErr != nil {
			statement.Close()
			return marshalErr
		}
		if _, err := statement.Exec(job.ID, result.RouteFingerprint, result.NodeName, string(resultTags), result.TargetID,
			connectivityResultVerdict(result), result.Success, result.LatencyMs, result.TestedAt.UnixNano()); err != nil {
			statement.Close()
			return err
		}
	}
	if err := statement.Close(); err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT id FROM connectivity_runs WHERE scope_key = ? ORDER BY completed_at DESC LIMIT -1 OFFSET ?`,
		connectivityScopeKey(job.Tags, job.Targets), connectivityHistoryRunsPerScope)
	if err != nil {
		return err
	}
	var expired []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		expired = append(expired, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range expired {
		if _, err := tx.Exec(`DELETE FROM connectivity_runs WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *stateDB) previousConnectivityRun(job ConnectivityJob) (storedConnectivityRun, bool, error) {
	var run storedConnectivityRun
	var completedAt int64
	err := s.db.QueryRow(`SELECT id, completed_at FROM connectivity_runs
		WHERE scope_key = ? AND id <> ? ORDER BY completed_at DESC LIMIT 1`,
		connectivityScopeKey(job.Tags, job.Targets), job.ID).Scan(&run.ID, &completedAt)
	if err == sql.ErrNoRows {
		return storedConnectivityRun{}, false, nil
	}
	if err != nil {
		return storedConnectivityRun{}, false, err
	}
	run.CompletedAt = time.Unix(0, completedAt)
	run.Results = make(map[string]ConnectivityResult)
	rows, err := s.db.Query(`SELECT route_fingerprint, node_name, tags_json, target_id, verdict,
		success, latency_ms, tested_at FROM connectivity_results WHERE run_id = ?`, run.ID)
	if err != nil {
		return storedConnectivityRun{}, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var result ConnectivityResult
		var tagsJSON, verdict string
		var success bool
		var testedAt int64
		if err := rows.Scan(&result.RouteFingerprint, &result.NodeName, &tagsJSON, &result.TargetID, &verdict,
			&success, &result.LatencyMs, &testedAt); err != nil {
			return storedConnectivityRun{}, false, err
		}
		if err := json.Unmarshal([]byte(tagsJSON), &result.Tags); err != nil {
			return storedConnectivityRun{}, false, fmt.Errorf("decode connectivity history tags: %w", err)
		}
		result.Verdict = ConnectivityVerdict(verdict)
		result.Success = success
		result.TestedAt = time.Unix(0, testedAt)
		run.Results[result.RouteFingerprint+"\x00"+result.TargetID] = result
	}
	return run, true, rows.Err()
}

func connectivityHistoryComparison(job ConnectivityJob, current map[string]ConnectivityResult, previous storedConnectivityRun, available bool) ConnectivityHistoryComparison {
	comparison := ConnectivityHistoryComparison{JobID: job.ID, Available: available, CurrentCompletedAt: job.UpdatedAt}
	if !available {
		return comparison
	}
	comparison.PreviousJobID = previous.ID
	comparison.PreviousCompletedAt = previous.CompletedAt
	for _, targetID := range job.Targets {
		counts := compareConnectivityResults(current, previous.Results, []string{targetID}, nil)
		comparison.Targets = append(comparison.Targets, ConnectivityHistoryTarget{TargetID: targetID, ConnectivityHistoryCounts: counts})
	}
	changes := make([]ConnectivityHistoryChange, 0)
	comparison.Overall = compareConnectivityResults(current, previous.Results, job.Targets, &changes)
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].NodeName == changes[j].NodeName {
			return changes[i].RouteFingerprint < changes[j].RouteFingerprint
		}
		return changes[i].NodeName < changes[j].NodeName
	})
	comparison.Changes = changes
	return comparison
}

type connectivityRouteVerdict struct {
	name   string
	tags   []string
	status string
}

func compareConnectivityResults(current, previous map[string]ConnectivityResult, targets []string, changes *[]ConnectivityHistoryChange) ConnectivityHistoryCounts {
	currentRoutes := aggregateConnectivityRoutes(current, targets)
	previousRoutes := aggregateConnectivityRoutes(previous, targets)
	keys := make(map[string]struct{}, len(currentRoutes)+len(previousRoutes))
	for key := range currentRoutes {
		keys[key] = struct{}{}
	}
	for key := range previousRoutes {
		keys[key] = struct{}{}
	}
	var counts ConnectivityHistoryCounts
	for key := range keys {
		currentResult, hasCurrent := currentRoutes[key]
		previousResult, hasPrevious := previousRoutes[key]
		switch {
		case !hasPrevious:
			counts.NoHistory++
		case !hasCurrent:
			counts.Removed++
		case previousResult.status == string(ConnectivityVerdictUsable) && currentResult.status == string(ConnectivityVerdictUsable):
			counts.ContinuedSuccess++
		case previousResult.status != string(ConnectivityVerdictUsable) && currentResult.status == string(ConnectivityVerdictUsable):
			counts.NewlySuccessful++
		case previousResult.status == string(ConnectivityVerdictUsable) && currentResult.status != string(ConnectivityVerdictUsable):
			counts.NewlyFailed++
		default:
			counts.ContinuedUnsuccessful++
		}
		if changes == nil || hasCurrent && hasPrevious && currentResult.status == previousResult.status {
			continue
		}
		name, tags := currentResult.name, currentResult.tags
		if !hasCurrent {
			name, tags = previousResult.name, previousResult.tags
		}
		previousStatus, currentStatus := previousResult.status, currentResult.status
		if !hasPrevious {
			previousStatus = "missing"
		}
		if !hasCurrent {
			currentStatus = "missing"
		}
		*changes = append(*changes, ConnectivityHistoryChange{RouteFingerprint: key, NodeName: name,
			Tags: append([]string(nil), tags...), Previous: previousStatus, Current: currentStatus})
	}
	return counts
}

func aggregateConnectivityRoutes(results map[string]ConnectivityResult, targets []string) map[string]connectivityRouteVerdict {
	targetSet := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		targetSet[target] = struct{}{}
	}
	type aggregate struct {
		name    string
		tags    []string
		seen    int
		usable  int
		partial bool
	}
	byRoute := make(map[string]*aggregate)
	for _, result := range results {
		if _, ok := targetSet[result.TargetID]; !ok {
			continue
		}
		item := byRoute[result.RouteFingerprint]
		if item == nil {
			item = &aggregate{name: result.NodeName, tags: append([]string(nil), result.Tags...)}
			byRoute[result.RouteFingerprint] = item
		}
		item.seen++
		if result.Success {
			item.usable++
		}
		if connectivityResultVerdict(result) == ConnectivityVerdictPartial {
			item.partial = true
		}
	}
	aggregated := make(map[string]connectivityRouteVerdict, len(byRoute))
	for fingerprint, item := range byRoute {
		status := string(ConnectivityVerdictFailed)
		if item.seen == len(targetSet) && item.usable == len(targetSet) {
			status = string(ConnectivityVerdictUsable)
		} else if item.partial || item.usable > 0 {
			status = string(ConnectivityVerdictPartial)
		}
		aggregated[fingerprint] = connectivityRouteVerdict{name: item.name, tags: item.tags, status: status}
	}
	return aggregated
}

func normalizeConnectivityTargets(targets []string) ([]string, error) {
	if len(targets) == 0 {
		targets = []string{"outlook"}
	}
	lower := make([]string, 0, len(targets))
	for _, target := range targets {
		lower = append(lower, strings.ToLower(strings.TrimSpace(target)))
	}
	normalized := normalizeConnectivityTags(lower)
	for _, id := range normalized {
		if _, ok := connectivityTargetByID(id); !ok {
			return nil, fmt.Errorf("不支持的站点 %q", id)
		}
	}
	return normalized, nil
}

func containsConnectivityTarget(targets []string, target string) bool {
	for _, item := range targets {
		if strings.EqualFold(item, target) {
			return true
		}
	}
	return false
}

func (s *Service) ConnectivityHistory(jobID string) (ConnectivityHistoryComparison, error) {
	s.connectivityJobsMu.RLock()
	state, ok := s.connectivityJobs[strings.TrimSpace(jobID)]
	if !ok || state.job.Status != ConnectivityJobFinished {
		s.connectivityJobsMu.RUnlock()
		return ConnectivityHistoryComparison{}, fmt.Errorf("检测任务不存在、未完成或已过期")
	}
	job := connectivityJobSnapshot(state)
	current := make(map[string]ConnectivityResult, len(state.results))
	for _, result := range state.results {
		current[result.RouteFingerprint+"\x00"+result.TargetID] = result
	}
	s.connectivityJobsMu.RUnlock()
	previous, available, err := s.store.db.previousConnectivityRun(job)
	if err != nil {
		return ConnectivityHistoryComparison{}, err
	}
	return connectivityHistoryComparison(job, current, previous, available), nil
}
