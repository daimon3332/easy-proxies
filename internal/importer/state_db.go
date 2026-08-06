package importer

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const stateSchemaVersion = 2

type stateDB struct {
	path           string
	db             *sql.DB
	closeOnce      sync.Once
	closeErr       error
	uiMu           sync.RWMutex
	uiSummaryCache *UISummary
	uiFilterCache  map[string]uiFilterValues
	uiRevision     uint64
}

func openStateDB(legacyPath string) (*stateDB, error) {
	dbPath := strings.TrimSuffix(legacyPath, filepath.Ext(legacyPath)) + ".db"
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	state := &stateDB{path: dbPath, db: db}
	if err := state.initialize(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := state.importLegacyOnce(legacyPath); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("checkpoint state database: %w", err)
	}
	return state, nil
}

func (s *stateDB) initialize() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, statement := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA cache_size=-1024",
		"PRAGMA wal_autocheckpoint=256",
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS store_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS managed_nodes (
			id TEXT PRIMARY KEY,
			state TEXT NOT NULL,
			in_pool INTEGER NOT NULL,
			order_index INTEGER NOT NULL,
			tag_prefix TEXT NOT NULL,
			import_id TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			original_name TEXT NOT NULL DEFAULT '',
			country_code TEXT NOT NULL DEFAULT '',
			country_name TEXT NOT NULL DEFAULT '',
			latency_ms INTEGER NOT NULL DEFAULT 0,
			port INTEGER NOT NULL DEFAULT 0,
			last_error TEXT NOT NULL DEFAULT '',
			import_source TEXT NOT NULL DEFAULT '',
			import_format TEXT NOT NULL DEFAULT '',
			updated_at INTEGER NOT NULL,
			payload BLOB NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS import_jobs (
			id TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			updated_at INTEGER NOT NULL,
			payload BLOB NOT NULL
		)`,
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize state database: %w", err)
		}
	}
	migrated, err := s.ensureManagedNodeUIColumns(ctx)
	if err != nil {
		return err
	}
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS idx_managed_nodes_state_pool ON managed_nodes(state, in_pool)`,
		`CREATE INDEX IF NOT EXISTS idx_managed_nodes_pool_order ON managed_nodes(in_pool, order_index)`,
		`CREATE INDEX IF NOT EXISTS idx_managed_nodes_tag ON managed_nodes(tag_prefix)`,
		`CREATE INDEX IF NOT EXISTS idx_managed_nodes_country ON managed_nodes(country_code)`,
		`CREATE INDEX IF NOT EXISTS idx_managed_nodes_latency ON managed_nodes(latency_ms)`,
		`CREATE INDEX IF NOT EXISTS idx_managed_nodes_name ON managed_nodes(name)`,
		`CREATE INDEX IF NOT EXISTS idx_ui_nodes_latency ON managed_nodes((latency_ms <= 0), latency_ms, name, id)`,
		`CREATE INDEX IF NOT EXISTS idx_ui_nodes_name ON managed_nodes((name = ''), name, id)`,
		`CREATE INDEX IF NOT EXISTS idx_ui_nodes_country ON managed_nodes((country_code = ''), country_code, name, id)`,
		`CREATE INDEX IF NOT EXISTS idx_ui_nodes_port ON managed_nodes((port <= 0), port, name, id)`,
		`CREATE INDEX IF NOT EXISTS idx_ui_nodes_tag ON managed_nodes((tag_prefix = ''), tag_prefix, name, id)`,
		`CREATE INDEX IF NOT EXISTS idx_import_jobs_updated ON import_jobs(updated_at DESC)`,
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize state database indexes: %w", err)
		}
	}
	if migrated {
		_, err = s.db.ExecContext(ctx, `UPDATE managed_nodes SET
			name = COALESCE(json_extract(payload, '$.name'), ''),
			original_name = COALESCE(json_extract(payload, '$.original_name'), ''),
			country_code = COALESCE(json_extract(payload, '$.country_code'), ''),
			country_name = COALESCE(json_extract(payload, '$.country_name'), ''),
			latency_ms = CAST(COALESCE(json_extract(payload, '$.latency_ms'), 0) AS INTEGER),
			port = CAST(COALESCE(json_extract(payload, '$.port'), 0) AS INTEGER),
			last_error = COALESCE(json_extract(payload, '$.last_error'), ''),
			import_source = COALESCE(json_extract(payload, '$.import_source'), ''),
			import_format = COALESCE(json_extract(payload, '$.import_format'), '')`)
		if err != nil {
			return fmt.Errorf("backfill state database UI columns: %w", err)
		}
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`,
		stateSchemaVersion, time.Now().UnixNano())
	return err
}

func (s *stateDB) ensureManagedNodeUIColumns(ctx context.Context) (bool, error) {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(managed_nodes)`)
	if err != nil {
		return false, err
	}
	columns := make(map[string]struct{})
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return false, err
		}
		columns[name] = struct{}{}
	}
	if err := rows.Close(); err != nil {
		return false, err
	}
	definitions := []string{
		"name TEXT NOT NULL DEFAULT ''",
		"original_name TEXT NOT NULL DEFAULT ''",
		"country_code TEXT NOT NULL DEFAULT ''",
		"country_name TEXT NOT NULL DEFAULT ''",
		"latency_ms INTEGER NOT NULL DEFAULT 0",
		"port INTEGER NOT NULL DEFAULT 0",
		"last_error TEXT NOT NULL DEFAULT ''",
		"import_source TEXT NOT NULL DEFAULT ''",
		"import_format TEXT NOT NULL DEFAULT ''",
	}
	migrated := false
	for _, definition := range definitions {
		name := strings.Fields(definition)[0]
		if _, ok := columns[name]; ok {
			continue
		}
		if _, err := s.db.ExecContext(ctx, "ALTER TABLE managed_nodes ADD COLUMN "+definition); err != nil {
			return false, err
		}
		migrated = true
	}
	return migrated, nil
}

func (s *stateDB) importLegacyOnce(path string) error {
	var marker string
	err := s.db.QueryRow(`SELECT value FROM store_meta WHERE key = 'legacy_json_imported'`).Scan(&marker)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	snapshot := storeFile{Version: storeVersion, Nodes: map[string]ManagedNode{}, Jobs: map[string]ImportJob{}}
	data, readErr := os.ReadFile(path)
	if readErr == nil {
		if err := json.Unmarshal(data, &snapshot); err != nil {
			return fmt.Errorf("decode legacy state: %w", err)
		}
		if snapshot.Nodes == nil {
			snapshot.Nodes = make(map[string]ManagedNode)
		}
		if snapshot.Jobs == nil {
			snapshot.Jobs = make(map[string]ImportJob)
		}
	} else if !os.IsNotExist(readErr) {
		return readErr
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := replaceStateTx(tx, snapshot.Nodes, snapshot.Jobs); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO store_meta(key, value) VALUES('legacy_json_imported', ?)`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *stateDB) load() (map[string]ManagedNode, map[string]ImportJob, error) {
	nodes := make(map[string]ManagedNode)
	rows, err := s.db.Query(`SELECT id, payload FROM managed_nodes`)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var id string
		var payload []byte
		if err := rows.Scan(&id, &payload); err != nil {
			rows.Close()
			return nil, nil, err
		}
		var node ManagedNode
		if err := json.Unmarshal(payload, &node); err != nil {
			rows.Close()
			return nil, nil, fmt.Errorf("decode node %s: %w", id, err)
		}
		nodes[id] = node
	}
	if err := rows.Close(); err != nil {
		return nil, nil, err
	}
	jobs := make(map[string]ImportJob)
	rows, err = s.db.Query(`SELECT id, payload FROM import_jobs`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var payload []byte
		if err := rows.Scan(&id, &payload); err != nil {
			return nil, nil, err
		}
		var job ImportJob
		if err := json.Unmarshal(payload, &job); err != nil {
			return nil, nil, fmt.Errorf("decode job %s: %w", id, err)
		}
		jobs[id] = job
	}
	return nodes, jobs, rows.Err()
}

func (s *stateDB) upsertNodes(nodes []ManagedNode) error {
	return s.write(func(tx *sql.Tx) error { return upsertNodesTx(tx, nodes) })
}

func (s *stateDB) deleteNodes(ids []string) error {
	return s.write(func(tx *sql.Tx) error {
		for _, id := range ids {
			if _, err := tx.Exec(`DELETE FROM managed_nodes WHERE id = ?`, id); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *stateDB) applyNodes(nodes []ManagedNode, deleted []string) error {
	return s.write(func(tx *sql.Tx) error {
		if err := upsertNodesTx(tx, nodes); err != nil {
			return err
		}
		for _, id := range deleted {
			if _, err := tx.Exec(`DELETE FROM managed_nodes WHERE id = ?`, id); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *stateDB) upsertJobs(jobs []ImportJob) error {
	return s.write(func(tx *sql.Tx) error { return upsertJobsTx(tx, jobs) })
}

func (s *stateDB) applyJobs(jobs []ImportJob, deleted []string) error {
	return s.write(func(tx *sql.Tx) error {
		if err := upsertJobsTx(tx, jobs); err != nil {
			return err
		}
		for _, id := range deleted {
			if _, err := tx.Exec(`DELETE FROM import_jobs WHERE id = ?`, id); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *stateDB) deleteJobs(ids []string) error {
	return s.write(func(tx *sql.Tx) error {
		for _, id := range ids {
			if _, err := tx.Exec(`DELETE FROM import_jobs WHERE id = ?`, id); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *stateDB) replace(nodes map[string]ManagedNode, jobs map[string]ImportJob) error {
	return s.write(func(tx *sql.Tx) error { return replaceStateTx(tx, nodes, jobs) })
}

func (s *stateDB) write(fn func(*sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.invalidateUICache()
	return nil
}

func (s *stateDB) invalidateUICache() {
	s.uiMu.Lock()
	s.uiRevision++
	s.uiSummaryCache = nil
	s.uiFilterCache = nil
	s.uiMu.Unlock()
}

func replaceStateTx(tx *sql.Tx, nodes map[string]ManagedNode, jobs map[string]ImportJob) error {
	if _, err := tx.Exec(`DELETE FROM managed_nodes`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM import_jobs`); err != nil {
		return err
	}
	nodeList := make([]ManagedNode, 0, len(nodes))
	for _, node := range nodes {
		nodeList = append(nodeList, node)
	}
	if err := upsertNodesTx(tx, nodeList); err != nil {
		return err
	}
	jobList := make([]ImportJob, 0, len(jobs))
	for _, job := range jobs {
		jobList = append(jobList, job)
	}
	return upsertJobsTx(tx, jobList)
}

func upsertNodesTx(tx *sql.Tx, nodes []ManagedNode) error {
	statement, err := tx.Prepare(`INSERT INTO managed_nodes(
		id, state, in_pool, order_index, tag_prefix, import_id, name, original_name,
		country_code, country_name, latency_ms, port, last_error, import_source, import_format, updated_at, payload)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET state=excluded.state, in_pool=excluded.in_pool,
		order_index=excluded.order_index, tag_prefix=excluded.tag_prefix, import_id=excluded.import_id,
		name=excluded.name, original_name=excluded.original_name, country_code=excluded.country_code,
		country_name=excluded.country_name, latency_ms=excluded.latency_ms, port=excluded.port,
		last_error=excluded.last_error, import_source=excluded.import_source, import_format=excluded.import_format,
		updated_at=excluded.updated_at, payload=excluded.payload`)
	if err != nil {
		return err
	}
	defer statement.Close()
	for _, node := range nodes {
		payload, err := json.Marshal(node)
		if err != nil {
			return err
		}
		if _, err := statement.Exec(
			node.ID, node.State, node.InPool, node.Order, node.TagPrefix, node.ImportID, node.Name, node.OriginalName,
			node.CountryCode, node.CountryName, node.LatencyMs, node.Port, node.LastError, node.ImportSource, node.ImportFormat,
			node.UpdatedAt.UnixNano(), payload,
		); err != nil {
			return err
		}
	}
	return nil
}

func upsertJobsTx(tx *sql.Tx, jobs []ImportJob) error {
	statement, err := tx.Prepare(`INSERT INTO import_jobs(id, status, updated_at, payload) VALUES(?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET status=excluded.status, updated_at=excluded.updated_at, payload=excluded.payload`)
	if err != nil {
		return err
	}
	defer statement.Close()
	for _, job := range jobs {
		job = compactPersistedJob(job)
		payload, err := json.Marshal(job)
		if err != nil {
			return err
		}
		if _, err := statement.Exec(job.ID, job.Status, job.UpdatedAt.UnixNano(), payload); err != nil {
			return err
		}
	}
	return nil
}

func (s *stateDB) close() error {
	if s == nil || s.db == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		s.closeErr = s.db.Close()
	})
	return s.closeErr
}
