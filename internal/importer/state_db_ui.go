package importer

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const (
	uiNameExpr        = `name`
	uiOriginalExpr    = `original_name`
	uiCountryExpr     = `country_code`
	uiCountryNameExpr = `country_name`
	uiLatencyExpr     = `latency_ms`
	uiPortExpr        = `port`
	uiSourceExpr      = `import_source`
	uiFormatExpr      = `import_format`
)

type uiFilterValues struct {
	countries []string
	tags      []string
}

func (s *stateDB) uiSummary() (UISummary, error) {
	s.uiMu.RLock()
	if s.uiSummaryCache != nil {
		summary := *s.uiSummaryCache
		s.uiMu.RUnlock()
		summary.UpdatedAt = time.Now()
		return summary, nil
	}
	revision := s.uiRevision
	s.uiMu.RUnlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var summary UISummary
	err := s.db.QueryRowContext(ctx, `SELECT
		COUNT(*),
		COALESCE(SUM(CASE WHEN in_pool = 1 OR state = ? THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN in_pool = 0 AND state <> ? AND state = ? THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN in_pool = 0 AND state <> ? AND state = ? THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN in_pool = 0 AND state <> ? AND state = ? THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN in_pool = 0 AND state <> ? AND state IN (?, ?) THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN in_pool = 0 AND state <> ? AND state = ? THEN 1 ELSE 0 END), 0)
		FROM managed_nodes`,
		StateInPool,
		StateInPool, StateParsed,
		StateInPool, StateTesting,
		StateInPool, StatePassed,
		StateInPool, StateFailed, StateBlocked,
		StateInPool, StateExcluded,
	).Scan(&summary.Total, &summary.InPool, &summary.Parsed, &summary.Testing, &summary.Passed, &summary.Failed, &summary.Excluded)
	if err != nil {
		return UISummary{}, err
	}
	summary.UpdatedAt = time.Now()
	s.uiMu.Lock()
	if revision == s.uiRevision {
		copySummary := summary
		s.uiSummaryCache = &copySummary
	}
	s.uiMu.Unlock()
	return summary, nil
}

func (s *stateDB) queryUINodes(query UINodeListQuery) ([]ManagedNode, int, []string, []string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	scopeSQL, scopeArgs := uiScopeSQL(query.Scope)
	countries, tags, err := s.uiFilters(ctx, query.Scope, scopeSQL, scopeArgs)
	if err != nil {
		return nil, 0, nil, nil, err
	}
	whereSQL, args := uiFilterSQL(query, scopeSQL, scopeArgs)
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM managed_nodes WHERE "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, nil, nil, err
	}
	orderExpr, emptyExpr := uiOrderSQL(query.Sort)
	direction := "ASC"
	if query.Order == "desc" {
		direction = "DESC"
	}
	statement := fmt.Sprintf(`SELECT id, original_name, name, tag_prefix, country_code, country_name,
		latency_ms, port, state, in_pool, order_index, last_error FROM managed_nodes WHERE %s
		ORDER BY (%s) ASC, %s %s, %s %s, id %s
		LIMIT ? OFFSET ?`, whereSQL, emptyExpr, orderExpr, direction, uiNameExpr, direction, direction)
	pageArgs := append(append([]any(nil), args...), query.PageSize, (query.Page-1)*query.PageSize)
	rows, err := s.db.QueryContext(ctx, statement, pageArgs...)
	if err != nil {
		return nil, 0, nil, nil, err
	}
	defer rows.Close()
	nodes := make([]ManagedNode, 0, query.PageSize)
	for rows.Next() {
		var node ManagedNode
		if err := rows.Scan(
			&node.ID, &node.OriginalName, &node.Name, &node.TagPrefix, &node.CountryCode, &node.CountryName,
			&node.LatencyMs, &node.Port, &node.State, &node.InPool, &node.Order, &node.LastError,
		); err != nil {
			return nil, 0, nil, nil, err
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, nil, nil, err
	}
	return nodes, total, countries, tags, nil
}

func (s *stateDB) uiFilters(ctx context.Context, scope, scopeSQL string, args []any) ([]string, []string, error) {
	s.uiMu.RLock()
	if cached, ok := s.uiFilterCache[scope]; ok {
		countries := append([]string(nil), cached.countries...)
		tags := append([]string(nil), cached.tags...)
		s.uiMu.RUnlock()
		return countries, tags, nil
	}
	revision := s.uiRevision
	s.uiMu.RUnlock()
	countries, err := s.uiDistinctValues(ctx, uiCountryExpr, scopeSQL, args)
	if err != nil {
		return nil, nil, err
	}
	tags, err := s.uiDistinctValues(ctx, "tag_prefix", scopeSQL, args)
	if err != nil {
		return nil, nil, err
	}
	s.uiMu.Lock()
	if revision == s.uiRevision {
		if s.uiFilterCache == nil {
			s.uiFilterCache = make(map[string]uiFilterValues)
		}
		s.uiFilterCache[scope] = uiFilterValues{countries: append([]string(nil), countries...), tags: append([]string(nil), tags...)}
	}
	s.uiMu.Unlock()
	return countries, tags, nil
}

func (s *stateDB) uiDistinctValues(ctx context.Context, expression, scopeSQL string, args []any) ([]string, error) {
	statement := fmt.Sprintf("SELECT DISTINCT %s FROM managed_nodes WHERE %s AND %s <> '' ORDER BY %s", expression, scopeSQL, expression, expression)
	rows, err := s.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]string, 0)
	for rows.Next() {
		var value sql.NullString
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		if value.Valid && value.String != "" {
			values = append(values, value.String)
		}
	}
	return values, rows.Err()
}

func uiScopeSQL(scope string) (string, []any) {
	switch scope {
	case "candidate":
		return "state = ? AND in_pool = 0", []any{StatePassed}
	case "pool":
		return "(in_pool = 1 OR state = ?)", []any{StateInPool}
	case "failed":
		return "state IN (?, ?)", []any{StateFailed, StateBlocked}
	default:
		return "1 = 1", nil
	}
}

func uiFilterSQL(query UINodeListQuery, scopeSQL string, scopeArgs []any) (string, []any) {
	clauses := []string{scopeSQL}
	args := append([]any(nil), scopeArgs...)
	if query.Country != "" {
		clauses = append(clauses, uiCountryExpr+" = ?")
		args = append(args, query.Country)
	}
	if query.Tag != "" {
		clauses = append(clauses, "tag_prefix = ?")
		args = append(args, query.Tag)
	}
	switch query.Latency {
	case "none":
		clauses = append(clauses, uiLatencyExpr+" <= 0")
	case "0-500":
		clauses = append(clauses, uiLatencyExpr+" > 0 AND "+uiLatencyExpr+" <= 500")
	case "500-1500":
		clauses = append(clauses, uiLatencyExpr+" > 500 AND "+uiLatencyExpr+" <= 1500")
	case "1500+":
		clauses = append(clauses, uiLatencyExpr+" > 1500")
	}
	if query.Query != "" {
		searchExpr := fmt.Sprintf("lower(%s || ' ' || %s || ' ' || tag_prefix || ' ' || %s || ' ' || %s || ' ' || %s || ' ' || %s || ' ' || state)", uiNameExpr, uiOriginalExpr, uiCountryExpr, uiCountryNameExpr, uiSourceExpr, uiFormatExpr)
		clauses = append(clauses, "instr("+searchExpr+", ?) > 0")
		args = append(args, query.Query)
	}
	return strings.Join(clauses, " AND "), args
}

func uiOrderSQL(field string) (string, string) {
	switch field {
	case "latency":
		return uiLatencyExpr, uiLatencyExpr + " <= 0"
	case "country":
		return uiCountryExpr, uiCountryExpr + " = ''"
	case "port":
		return uiPortExpr, uiPortExpr + " <= 0"
	case "tag":
		return "tag_prefix", "tag_prefix = ''"
	default:
		return uiNameExpr, uiNameExpr + " = ''"
	}
}
