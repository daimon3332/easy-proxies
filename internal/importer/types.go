package importer

import (
	"time"

	"easy_proxies/internal/config"
	"easy_proxies/internal/proxychain"
)

type ManagedNodeState string

const (
	StateParsed   ManagedNodeState = "parsed"
	StateTesting  ManagedNodeState = "testing"
	StatePassed   ManagedNodeState = "passed"
	StateFailed   ManagedNodeState = "failed"
	StateBlocked  ManagedNodeState = "blocked_by_chain"
	StateInPool   ManagedNodeState = "in_pool"
	StateExcluded ManagedNodeState = "excluded"
)

type ManagedNode struct {
	ID                  string           `json:"id"`
	URI                 string           `json:"uri"`
	ChainProfileID      string           `json:"chain_profile_id,omitempty"`
	OriginalName        string           `json:"original_name"`
	Name                string           `json:"name"`
	TagPrefix           string           `json:"tag_prefix"`
	ImportID            string           `json:"import_id,omitempty"`
	ImportMode          string           `json:"import_mode,omitempty"`
	ImportSource        string           `json:"import_source,omitempty"`
	ImportFormat        string           `json:"import_format,omitempty"`
	SourceRefs          []NodeSourceRef  `json:"source_refs,omitempty"`
	CountryCode         string           `json:"country_code,omitempty"`
	CountryName         string           `json:"country_name,omitempty"`
	LatencyMs           int64            `json:"latency_ms,omitempty"`
	Port                uint16           `json:"port,omitempty"`
	State               ManagedNodeState `json:"state"`
	Enabled             bool             `json:"enabled"`
	InPool              bool             `json:"in_pool"`
	Order               int              `json:"order"`
	ConsecutiveFailures int              `json:"consecutive_failures,omitempty"`
	LastError           string           `json:"last_error,omitempty"`
	LastTestAt          time.Time        `json:"last_test_at,omitempty"`
	CreatedAt           time.Time        `json:"created_at"`
	UpdatedAt           time.Time        `json:"updated_at"`
}

type NodeSourceRef struct {
	TagPrefix      string `json:"tag_prefix"`
	ImportID       string `json:"import_id,omitempty"`
	Mode           string `json:"mode,omitempty"`
	Source         string `json:"source,omitempty"`
	Format         string `json:"format,omitempty"`
	ChainProfileID string `json:"chain_profile_id,omitempty"`
	FetchPolicy    string `json:"fetch_policy,omitempty"`
}

func (n ManagedNode) ToConfigNode() config.NodeConfig {
	return config.NodeConfig{
		Name:           n.Name,
		URI:            n.URI,
		ChainProfileID: n.ChainProfileID,
		Port:           n.Port,
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
	ID               string             `json:"id"`
	Status           ImportStatus       `json:"status"`
	Mode             string             `json:"mode,omitempty"`
	Format           string             `json:"format,omitempty"`
	TagPrefix        string             `json:"tag_prefix,omitempty"`
	Source           string             `json:"source,omitempty"`
	SourceRevision   uint64             `json:"source_revision,omitempty"`
	ChainProfileID   string             `json:"chain_profile_id,omitempty"`
	FetchPolicy      string             `json:"fetch_policy,omitempty"`
	Total            int                `json:"total"`
	Passed           int                `json:"passed"`
	Failed           int                `json:"failed"`
	Promoted         int                `json:"promoted"`
	ProbeRound       int                `json:"probe_round,omitempty"`
	ProbeRounds      int                `json:"probe_rounds,omitempty"`
	ProbeRoundDone   int                `json:"probe_round_done,omitempty"`
	ProbeRoundTotal  int                `json:"probe_round_total,omitempty"`
	ProbePending     int                `json:"probe_pending,omitempty"`
	ProbeTarget      string             `json:"probe_target,omitempty"`
	ProbeConcurrency int                `json:"probe_concurrency,omitempty"`
	Test204          bool               `json:"test_204"`
	SiteTargets      []string           `json:"site_targets,omitempty"`
	SiteProgress     []SiteTestProgress `json:"site_progress,omitempty"`
	Detail           string             `json:"detail,omitempty"`
	Error            string             `json:"error,omitempty"`
	ChainProbe       *ChainProbeResult  `json:"chain_probe,omitempty"`
	NodeIDs          []string           `json:"node_ids"`
	CreatedAt        time.Time          `json:"created_at"`
	UpdatedAt        time.Time          `json:"updated_at"`
}

type ParseRequest struct {
	Mode           string `json:"mode"`
	URL            string `json:"url,omitempty"`
	Content        string `json:"content,omitempty"`
	TagPrefix      string `json:"tag_prefix,omitempty"`
	ChainProfileID string `json:"chain_profile_id,omitempty"`
	FetchPolicy    string `json:"fetch_policy,omitempty"`
	ContentFormat  string `json:"content_format,omitempty"`
	ProxyProtocol  string `json:"proxy_protocol,omitempty"`
}

type ParseResponse struct {
	ImportID string        `json:"import_id"`
	Format   string        `json:"format"`
	Nodes    []ManagedNode `json:"nodes"`
}

type ImportSourceSummary struct {
	Key            string    `json:"key"`
	ImportID       string    `json:"import_id,omitempty"`
	Mode           string    `json:"mode"`
	Format         string    `json:"format"`
	TagPrefix      string    `json:"tag_prefix"`
	Source         string    `json:"source"`
	Total          int       `json:"total"`
	Pool           int       `json:"pool"`
	Candidate      int       `json:"candidate"`
	Failed         int       `json:"failed"`
	Refreshable    bool      `json:"refreshable"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	ChainProfileID string    `json:"chain_profile_id,omitempty"`
	ChainBinding   string    `json:"chain_binding"`
	FetchPolicy    string    `json:"fetch_policy,omitempty"`
}

const (
	ChainBindingDirect  = "direct"
	ChainBindingProfile = "profile"
	ChainBindingMixed   = "mixed"
)

type TagBindingRequest struct {
	Tags           []string `json:"tags"`
	ChainProfileID string   `json:"chain_profile_id,omitempty"`
	Test204        *bool    `json:"test_204,omitempty"`
	SiteTargets    []string `json:"site_targets,omitempty"`
}

type TagBindingItem struct {
	TagPrefix string    `json:"tag_prefix"`
	Status    string    `json:"status"`
	JobID     string    `json:"job_id,omitempty"`
	Total     int       `json:"total"`
	Passed    int       `json:"passed"`
	Failed    int       `json:"failed"`
	Promoted  int       `json:"promoted"`
	Error     string    `json:"error,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type TagBindingJob struct {
	ID             string           `json:"id"`
	Status         string           `json:"status"`
	ChainProfileID string           `json:"chain_profile_id,omitempty"`
	Test204        bool             `json:"test_204"`
	SiteTargets    []string         `json:"site_targets,omitempty"`
	TotalTags      int              `json:"total_tags"`
	DoneTags       int              `json:"done_tags"`
	Successful     int              `json:"successful"`
	Failed         int              `json:"failed"`
	Skipped        int              `json:"skipped"`
	Items          []TagBindingItem `json:"items"`
	Error          string           `json:"error,omitempty"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
}

type ChainProbeResult struct {
	ProfileID   string `json:"profile_id"`
	ProfileName string `json:"profile_name"`
	LatencyMs   int64  `json:"latency_ms,omitempty"`
	Error       string `json:"error,omitempty"`
}

type ChainProfileResponse struct {
	Profiles     []proxychain.Profile         `json:"profiles"`
	Usage        map[string]ChainProfileUsage `json:"usage,omitempty"`
	RetestJobID  string                       `json:"retest_job_id,omitempty"`
	DeletedNodes int                          `json:"deleted_nodes,omitempty"`
}

type ChainProfileUsage struct {
	Nodes int `json:"nodes"`
	Ports int `json:"ports"`
}

type ChainProfileMutationRequest struct {
	Profiles []proxychain.Profile `json:"profiles"`
	Cascade  bool                 `json:"cascade,omitempty"`
}

type ConnectivityTarget struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Host string `json:"host"`
	Port uint16 `json:"port"`
	URL  string `json:"url"`
}

type ConnectivityTagScope struct {
	Tag   string `json:"tag"`
	Nodes int    `json:"nodes"`
}

type ConnectivityScopeResponse struct {
	Targets []ConnectivityTarget   `json:"targets"`
	Tags    []ConnectivityTagScope `json:"tags"`
}

type ConnectivityStartRequest struct {
	Tags           []string `json:"tags"`
	Targets        []string `json:"targets,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
}

type ConnectivityJobStatus string

const (
	ConnectivityJobRunning  ConnectivityJobStatus = "running"
	ConnectivityJobFinished ConnectivityJobStatus = "finished"
	ConnectivityJobFailed   ConnectivityJobStatus = "failed"
	ConnectivityJobCanceled ConnectivityJobStatus = "canceled"
)

type ConnectivityTargetSummary struct {
	TargetID        string `json:"target_id"`
	Total           int    `json:"total"`
	FirstPassed     int    `json:"first_passed"`
	Passed          int    `json:"passed"`
	Partial         int    `json:"partial"`
	Failed          int    `json:"failed"`
	Retried         int    `json:"retried"`
	Recovered       int    `json:"recovered"`
	MedianLatencyMs int64  `json:"median_latency_ms,omitempty"`
}

type ConnectivityTagSummary struct {
	Tag     string                      `json:"tag"`
	Routes  int                         `json:"routes"`
	Targets []ConnectivityTargetSummary `json:"targets"`
}

type ConnectivityJob struct {
	ID             string                   `json:"id"`
	Status         ConnectivityJobStatus    `json:"status"`
	Phase          string                   `json:"phase"`
	Tags           []string                 `json:"tags"`
	Targets        []string                 `json:"targets"`
	TotalRoutes    int                      `json:"total_routes"`
	TotalChecks    int                      `json:"total_checks"`
	DoneChecks     int                      `json:"done_checks"`
	RetryChecks    int                      `json:"retry_checks"`
	RetryDone      int                      `json:"retry_done"`
	Recovered      int                      `json:"recovered"`
	Concurrency    int                      `json:"concurrency"`
	TimeoutSeconds int                      `json:"timeout_seconds"`
	Summaries      []ConnectivityTagSummary `json:"summaries"`
	Error          string                   `json:"error,omitempty"`
	HistoryError   string                   `json:"history_error,omitempty"`
	StartedAt      time.Time                `json:"started_at"`
	UpdatedAt      time.Time                `json:"updated_at"`
}

type ConnectivityResult struct {
	NodeID           string                        `json:"node_id"`
	NodeName         string                        `json:"node_name"`
	Tags             []string                      `json:"tags"`
	RouteFingerprint string                        `json:"route_fingerprint"`
	TargetID         string                        `json:"target_id"`
	Verdict          ConnectivityVerdict           `json:"verdict"`
	FirstSuccess     bool                          `json:"first_success"`
	Success          bool                          `json:"success"`
	Attempts         int                           `json:"attempts"`
	LatencyMs        int64                         `json:"latency_ms,omitempty"`
	TLSVersion       string                        `json:"tls_version,omitempty"`
	HTTPStatus       int                           `json:"http_status,omitempty"`
	FinalHost        string                        `json:"final_host,omitempty"`
	ContentType      string                        `json:"content_type,omitempty"`
	InspectedBytes   int64                         `json:"inspected_bytes,omitempty"`
	Components       []ConnectivityComponentResult `json:"components,omitempty"`
	FailureStage     string                        `json:"failure_stage,omitempty"`
	Error            string                        `json:"error,omitempty"`
	Retryable        bool                          `json:"-"`
	TestedAt         time.Time                     `json:"tested_at"`
}

type ConnectivityVerdict string

const (
	ConnectivityVerdictUsable  ConnectivityVerdict = "usable"
	ConnectivityVerdictPartial ConnectivityVerdict = "partial"
	ConnectivityVerdictFailed  ConnectivityVerdict = "failed"
)

type ConnectivityComponentResult struct {
	ID             string              `json:"id"`
	Name           string              `json:"name"`
	Verdict        ConnectivityVerdict `json:"verdict"`
	Success        bool                `json:"success"`
	Attempts       int                 `json:"attempts"`
	LatencyMs      int64               `json:"latency_ms,omitempty"`
	TLSVersion     string              `json:"tls_version,omitempty"`
	HTTPStatus     int                 `json:"http_status,omitempty"`
	FinalHost      string              `json:"final_host,omitempty"`
	ContentType    string              `json:"content_type,omitempty"`
	InspectedBytes int64               `json:"inspected_bytes,omitempty"`
	FailureStage   string              `json:"failure_stage,omitempty"`
	Error          string              `json:"error,omitempty"`
	Retryable      bool                `json:"-"`
}

type ConnectivityResultQuery struct {
	JobID    string
	Tag      string
	TargetID string
	Status   string
	Page     int
	PageSize int
}

type ConnectivityResultPage struct {
	Items    []ConnectivityResult `json:"items"`
	Total    int                  `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
}

type ConnectivityPortRequest struct {
	JobID        string   `json:"job_id"`
	Tags         []string `json:"tags"`
	Targets      []string `json:"targets"`
	PreviewToken string   `json:"preview_token,omitempty"`
	AllowEmpty   bool     `json:"allow_empty,omitempty"`
}

type ConnectivityPortChangeItem struct {
	NodeName string           `json:"node_name"`
	Tags     []string         `json:"tags"`
	Port     uint16           `json:"port,omitempty"`
	State    ManagedNodeState `json:"state"`
}

type ConnectivityPortPreview struct {
	JobID          string                       `json:"job_id"`
	Tags           []string                     `json:"tags"`
	Targets        []string                     `json:"targets"`
	Qualifying     int                          `json:"qualifying"`
	NonQualifying  int                          `json:"non_qualifying"`
	WillFail       int                          `json:"will_fail"`
	CurrentPool    int                          `json:"current_pool"`
	ProjectedPool  int                          `json:"projected_pool"`
	Retained       int                          `json:"retained"`
	Added          int                          `json:"added"`
	Removed        int                          `json:"removed"`
	Unaffected     int                          `json:"unaffected"`
	SharedRetained int                          `json:"shared_retained"`
	Stale          int                          `json:"stale"`
	EmptyBlocked   bool                         `json:"empty_blocked"`
	PreviewToken   string                       `json:"preview_token"`
	AddedItems     []ConnectivityPortChangeItem `json:"added_items,omitempty"`
	RemovedItems   []ConnectivityPortChangeItem `json:"removed_items,omitempty"`
}

type ConnectivityPortApplyResponse struct {
	ConnectivityPortPreview
	PoolCount int `json:"pool_count"`
}

type ConnectivityHistoryCounts struct {
	ContinuedSuccess      int `json:"continued_success"`
	NewlySuccessful       int `json:"newly_successful"`
	NewlyFailed           int `json:"newly_failed"`
	ContinuedUnsuccessful int `json:"continued_unsuccessful"`
	NoHistory             int `json:"no_history"`
	Removed               int `json:"removed"`
}

type ConnectivityHistoryTarget struct {
	TargetID string `json:"target_id"`
	ConnectivityHistoryCounts
}

type ConnectivityHistoryChange struct {
	RouteFingerprint string   `json:"route_fingerprint"`
	NodeName         string   `json:"node_name"`
	Tags             []string `json:"tags"`
	Previous         string   `json:"previous"`
	Current          string   `json:"current"`
}

type ConnectivityHistoryComparison struct {
	JobID               string                      `json:"job_id"`
	Available           bool                        `json:"available"`
	PreviousJobID       string                      `json:"previous_job_id,omitempty"`
	PreviousCompletedAt time.Time                   `json:"previous_completed_at,omitempty"`
	CurrentCompletedAt  time.Time                   `json:"current_completed_at"`
	Overall             ConnectivityHistoryCounts   `json:"overall"`
	Targets             []ConnectivityHistoryTarget `json:"targets"`
	Changes             []ConnectivityHistoryChange `json:"changes,omitempty"`
}

type DashboardSummary struct {
	Total     int           `json:"total"`
	Parsed    int           `json:"parsed"`
	Testing   int           `json:"testing"`
	Passed    int           `json:"passed"`
	Failed    int           `json:"failed"`
	InPool    int           `json:"in_pool"`
	Excluded  int           `json:"excluded"`
	Ports     []ManagedNode `json:"ports"`
	UpdatedAt time.Time     `json:"updated_at"`
}

type UISummary struct {
	Total     int       `json:"total"`
	Parsed    int       `json:"parsed"`
	Testing   int       `json:"testing"`
	Passed    int       `json:"passed"`
	Failed    int       `json:"failed"`
	InPool    int       `json:"in_pool"`
	Excluded  int       `json:"excluded"`
	UpdatedAt time.Time `json:"updated_at"`
}

type UINodeListQuery struct {
	Scope    string
	Country  string
	Tag      string
	Query    string
	Latency  string
	Sort     string
	Order    string
	Page     int
	PageSize int
}

type UINodeListItem struct {
	ID           string           `json:"id"`
	OriginalName string           `json:"original_name,omitempty"`
	Name         string           `json:"name"`
	TagPrefix    string           `json:"tag_prefix,omitempty"`
	CountryCode  string           `json:"country_code,omitempty"`
	CountryName  string           `json:"country_name,omitempty"`
	LatencyMs    int64            `json:"latency_ms,omitempty"`
	Port         uint16           `json:"port,omitempty"`
	State        ManagedNodeState `json:"state"`
	InPool       bool             `json:"in_pool"`
	Order        int              `json:"order"`
	LastError    string           `json:"last_error,omitempty"`
}

type UINodeListResponse struct {
	Items     []UINodeListItem `json:"items"`
	Total     int              `json:"total"`
	Page      int              `json:"page"`
	PageSize  int              `json:"page_size"`
	Countries []string         `json:"countries"`
	Tags      []string         `json:"tags"`
	UpdatedAt time.Time        `json:"updated_at"`
}

type UIPortPreviewItem struct {
	Name        string `json:"name"`
	TagPrefix   string `json:"tag_prefix,omitempty"`
	CountryCode string `json:"country_code,omitempty"`
	Port        uint16 `json:"port"`
}

type CommitRequest struct {
	NodeIDs       []string `json:"node_ids,omitempty"`
	AutoReload    bool     `json:"auto_reload"`
	PromotePassed bool     `json:"promote_passed"`
	Test204       *bool    `json:"test_204,omitempty"`
	SiteTargets   []string `json:"site_targets,omitempty"`
}

type CommitResponse struct {
	JobID string `json:"job_id"`
}

type BatchTestRequest struct {
	NodeIDs       []string `json:"node_ids"`
	Scopes        []string `json:"scopes,omitempty"`
	Retest        bool     `json:"retest"`
	Country       bool     `json:"country"`
	PromotePassed bool     `json:"promote_passed"`
	AutoReload    bool     `json:"auto_reload"`
	ParentRefresh bool     `json:"-"`
	Test204       *bool    `json:"test_204,omitempty"`
	SiteTargets   []string `json:"site_targets,omitempty"`
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

type ProbeRoundProgress struct {
	Round       int
	Rounds      int
	Completed   int
	Total       int
	Pending     int
	Target      string
	Concurrency int
}

type VerificationPolicy struct {
	Test204     bool     `json:"test_204"`
	SiteTargets []string `json:"site_targets,omitempty"`
}

type SiteTestProgress struct {
	TargetID  string `json:"target_id"`
	Name      string `json:"name"`
	Total     int    `json:"total"`
	Done      int    `json:"done"`
	Passed    int    `json:"passed"`
	Failed    int    `json:"failed"`
	Retried   int    `json:"retried,omitempty"`
	Recovered int    `json:"recovered,omitempty"`
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
	ID               string             `json:"id"`
	Status           TestJobStatus      `json:"status"`
	Total            int                `json:"total"`
	Done             int                `json:"done"`
	Passed           int                `json:"passed"`
	Failed           int                `json:"failed"`
	CountryOK        int                `json:"country_ok"`
	CountryBad       int                `json:"country_bad"`
	Promoted         int                `json:"promoted"`
	ProbeRound       int                `json:"probe_round,omitempty"`
	ProbeRounds      int                `json:"probe_rounds,omitempty"`
	ProbeRoundDone   int                `json:"probe_round_done,omitempty"`
	ProbeRoundTotal  int                `json:"probe_round_total,omitempty"`
	ProbePending     int                `json:"probe_pending,omitempty"`
	ProbeTarget      string             `json:"probe_target,omitempty"`
	ProbeConcurrency int                `json:"probe_concurrency,omitempty"`
	Test204          bool               `json:"test_204"`
	SiteTargets      []string           `json:"site_targets,omitempty"`
	SiteProgress     []SiteTestProgress `json:"site_progress,omitempty"`
	Applied          bool               `json:"applied"`
	Protected        bool               `json:"protected,omitempty"`
	ProtectionReason string             `json:"protection_reason,omitempty"`
	Phase            string             `json:"phase"`
	Error            string             `json:"error,omitempty"`
	ChainProbes      []ChainProbeResult `json:"chain_probes,omitempty"`
	StartedAt        time.Time          `json:"started_at"`
	UpdatedAt        time.Time          `json:"updated_at"`
}

type SourceRefreshJobStatus string

const (
	SourceRefreshJobRunning  SourceRefreshJobStatus = "running"
	SourceRefreshJobFinished SourceRefreshJobStatus = "finished"
	SourceRefreshJobFailed   SourceRefreshJobStatus = "failed"
	SourceRefreshJobCanceled SourceRefreshJobStatus = "canceled"
)

type SourceRefreshURL struct {
	URL              string             `json:"url"`
	Kind             string             `json:"kind,omitempty"`
	Label            string             `json:"label,omitempty"`
	Status           string             `json:"status"`
	Nodes            int                `json:"nodes"`
	Done             int                `json:"done"`
	Total            int                `json:"total"`
	Passed           int                `json:"passed"`
	Failed           int                `json:"failed"`
	Promoted         int                `json:"promoted"`
	Stage            string             `json:"stage,omitempty"`
	Detail           string             `json:"detail,omitempty"`
	Warning          string             `json:"warning,omitempty"`
	Attempt          int                `json:"attempt,omitempty"`
	Attempts         int                `json:"attempts,omitempty"`
	Cached           bool               `json:"cached,omitempty"`
	ProbeRound       int                `json:"probe_round,omitempty"`
	ProbeRounds      int                `json:"probe_rounds,omitempty"`
	ProbeRoundDone   int                `json:"probe_round_done,omitempty"`
	ProbeRoundTotal  int                `json:"probe_round_total,omitempty"`
	ProbePending     int                `json:"probe_pending,omitempty"`
	ProbeTarget      string             `json:"probe_target,omitempty"`
	ProbeConcurrency int                `json:"probe_concurrency,omitempty"`
	SiteProgress     []SiteTestProgress `json:"site_progress,omitempty"`
	ChainProbe       *ChainProbeResult  `json:"chain_probe,omitempty"`
	Protected        bool               `json:"protected,omitempty"`
	Error            string             `json:"error,omitempty"`
	UpdatedAt        time.Time          `json:"updated_at"`
}

type SourceRefreshGroup struct {
	Key        string             `json:"key"`
	TagPrefix  string             `json:"tag_prefix"`
	Done       int                `json:"done"`
	Total      int                `json:"total"`
	Successful int                `json:"successful"`
	Failed     int                `json:"failed"`
	URLs       []SourceRefreshURL `json:"urls"`
}

type SourceRefreshJob struct {
	ID               string                 `json:"id"`
	Status           SourceRefreshJobStatus `json:"status"`
	Phase            string                 `json:"phase"`
	TotalURLs        int                    `json:"total_urls"`
	DoneURLs         int                    `json:"done_urls"`
	Successful       int                    `json:"successful"`
	Failed           int                    `json:"failed"`
	TotalNodes       int                    `json:"total_nodes"`
	DoneNodes        int                    `json:"done_nodes"`
	Passed           int                    `json:"passed"`
	ProbePassed      int                    `json:"probe_passed"`
	FailedNodes      int                    `json:"failed_nodes"`
	Promoted         int                    `json:"promoted"`
	PoolCount        int                    `json:"pool_count"`
	InitialPoolCount int                    `json:"initial_pool_count"`
	Applied          bool                   `json:"applied"`
	Protected        bool                   `json:"protected,omitempty"`
	ProtectionReason string                 `json:"protection_reason,omitempty"`
	Test204          bool                   `json:"test_204"`
	SiteTargets      []string               `json:"site_targets,omitempty"`
	Groups           []SourceRefreshGroup   `json:"groups"`
	Error            string                 `json:"error,omitempty"`
	StartedAt        time.Time              `json:"started_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
}
