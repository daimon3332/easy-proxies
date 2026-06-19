package importer

import (
	"time"

	"easy_proxies/internal/config"
)

type ManagedNodeState string

const (
	StateParsed   ManagedNodeState = "parsed"
	StateTesting  ManagedNodeState = "testing"
	StatePassed   ManagedNodeState = "passed"
	StateFailed   ManagedNodeState = "failed"
	StateInPool   ManagedNodeState = "in_pool"
	StateExcluded ManagedNodeState = "excluded"
)

type ManagedNode struct {
	ID           string           `json:"id"`
	URI          string           `json:"uri"`
	OriginalName string           `json:"original_name"`
	Name         string           `json:"name"`
	TagPrefix    string           `json:"tag_prefix"`
	ImportID     string           `json:"import_id,omitempty"`
	ImportMode   string           `json:"import_mode,omitempty"`
	ImportSource string           `json:"import_source,omitempty"`
	ImportFormat string           `json:"import_format,omitempty"`
	CountryCode  string           `json:"country_code,omitempty"`
	CountryName  string           `json:"country_name,omitempty"`
	LatencyMs    int64            `json:"latency_ms,omitempty"`
	Port         uint16           `json:"port,omitempty"`
	State        ManagedNodeState `json:"state"`
	Enabled      bool             `json:"enabled"`
	InPool       bool             `json:"in_pool"`
	Order        int              `json:"order"`
	LastError    string           `json:"last_error,omitempty"`
	LastTestAt   time.Time        `json:"last_test_at,omitempty"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
}

func (n ManagedNode) ToConfigNode() config.NodeConfig {
	return config.NodeConfig{
		Name: n.Name,
		URI:  n.URI,
		Port: n.Port,
	}
}

type ImportStatus string

const (
	ImportStatusParsed    ImportStatus = "parsed"
	ImportStatusRunning   ImportStatus = "running"
	ImportStatusCompleted ImportStatus = "completed"
	ImportStatusFailed    ImportStatus = "failed"
	ImportStatusCanceled  ImportStatus = "canceled"
)

type ImportJob struct {
	ID        string       `json:"id"`
	Status    ImportStatus `json:"status"`
	Mode      string       `json:"mode,omitempty"`
	Format    string       `json:"format,omitempty"`
	TagPrefix string       `json:"tag_prefix,omitempty"`
	Source    string       `json:"source,omitempty"`
	Total     int          `json:"total"`
	Passed    int          `json:"passed"`
	Failed    int          `json:"failed"`
	Promoted  int          `json:"promoted"`
	Detail    string       `json:"detail,omitempty"`
	Error     string       `json:"error,omitempty"`
	NodeIDs   []string     `json:"node_ids"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

type ParseRequest struct {
	Mode      string `json:"mode"`
	URL       string `json:"url,omitempty"`
	Content   string `json:"content,omitempty"`
	TagPrefix string `json:"tag_prefix,omitempty"`
}

type ParseResponse struct {
	ImportID string        `json:"import_id"`
	Format   string        `json:"format"`
	Nodes    []ManagedNode `json:"nodes"`
}

type ImportSourceSummary struct {
	Key         string    `json:"key"`
	ImportID    string    `json:"import_id,omitempty"`
	Mode        string    `json:"mode"`
	Format      string    `json:"format"`
	TagPrefix   string    `json:"tag_prefix"`
	Source      string    `json:"source"`
	Total       int       `json:"total"`
	Pool        int       `json:"pool"`
	Candidate   int       `json:"candidate"`
	Failed      int       `json:"failed"`
	Refreshable bool      `json:"refreshable"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CommitRequest struct {
	NodeIDs       []string `json:"node_ids,omitempty"`
	AutoReload    bool     `json:"auto_reload"`
	PromotePassed bool     `json:"promote_passed"`
}

type CommitResponse struct {
	JobID string `json:"job_id"`
}

type BatchTestRequest struct {
	NodeIDs       []string `json:"node_ids"`
	Retest        bool     `json:"retest"`
	Country       bool     `json:"country"`
	PromotePassed bool     `json:"promote_passed"`
	AutoReload    bool     `json:"auto_reload"`
}

type BatchTestResponse struct {
	Total      int           `json:"total"`
	Retested   int           `json:"retested"`
	Passed     int           `json:"passed"`
	Failed     int           `json:"failed"`
	CountryOK  int           `json:"country_ok"`
	CountryBad int           `json:"country_bad"`
	Promoted   int           `json:"promoted"`
	Nodes      []ManagedNode `json:"nodes"`
}

type TestResult struct {
	LatencyMs   int64
	CountryCode string
	CountryName string
	Error       error
}

type NodeTestEvent struct {
	NodeID string
	Result TestResult
}

// TestJobStatus reflects the lifecycle of an async batch test.
type TestJobStatus string

const (
	TestJobRunning  TestJobStatus = "running"
	TestJobFinished TestJobStatus = "finished"
	TestJobFailed   TestJobStatus = "failed"
	TestJobCanceled TestJobStatus = "canceled"
)

// TestJob is a snapshot of an async batch test exposed over the WebUI polling
// endpoint. Counts are cumulative across the probe and country phases.
type TestJob struct {
	ID         string        `json:"id"`
	Status     TestJobStatus `json:"status"`
	Total      int           `json:"total"`
	Done       int           `json:"done"`
	Passed     int           `json:"passed"`
	Failed     int           `json:"failed"`
	CountryOK  int           `json:"country_ok"`
	CountryBad int           `json:"country_bad"`
	Promoted   int           `json:"promoted"`
	Phase      string        `json:"phase"`
	Error      string        `json:"error,omitempty"`
	StartedAt  time.Time     `json:"started_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
}
