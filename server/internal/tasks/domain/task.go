package domain

import (
	"bytes"
	"encoding/json"

	billingdomain "github.com/QuantumNous/new-api/internal/billing/domain"
)

type TaskStatus string

const (
	TaskStatusNotStart   TaskStatus = "NOT_START"
	TaskStatusSubmitted             = "SUBMITTED"
	TaskStatusQueued                = "QUEUED"
	TaskStatusInProgress            = "IN_PROGRESS"
	TaskStatusFailure               = "FAILURE"
	TaskStatusSuccess               = "SUCCESS"
	TaskStatusUnknown               = "UNKNOWN"
)

// TaskRefundLegacyCutoff separates tasks created before timeout refunds were
// introduced. Those legacy tasks are failed without an automatic refund.
const TaskRefundLegacyCutoff int64 = 1771718400 // 2026-02-22 00:00:00 UTC

type Task struct {
	ID         int64
	CreatedAt  int64
	UpdatedAt  int64
	TaskID     string
	Platform   string
	UserId     int
	Group      string
	ChannelId  int
	Quota      int
	Action     string
	Status     TaskStatus
	FailReason string
	SubmitTime int64
	StartTime  int64
	FinishTime int64
	Progress   string
	Properties Properties
	Username   string

	PrivateData TaskPrivateData
	Data        json.RawMessage
}

type Properties struct {
	Input             string
	UpstreamModelName string
	OriginModelName   string
}

type TaskPrivateData struct {
	Key            string
	UpstreamTaskID string
	ResultURL      string
	// Execution records safe, immutable request provenance. It lives next to
	// other private task state so public task DTOs cannot expose it by accident.
	Execution *TaskExecutionSnapshot

	BillingSource  string
	SubscriptionId int
	TokenId        int
	NodeName       string
	BillingContext *TaskBillingContext
	// ResponsesBackground records that the openai_responses create request
	// asked for background:true. Every task is durable and survives client
	// disconnect regardless; this only echoes the protocol-level request
	// attribute back on retrieval snapshots.
	ResponsesBackground bool
	// PluginState is plugin-owned cross-round data. Unlike Task.Data it is
	// only replaced when a hook explicitly returns state.
	PluginState json.RawMessage
	// PollFailures counts consecutive unrecognized or transient poll outcomes.
	PollFailures    int
	Reconciliation  *TaskReconciliation
	OpenRouterAudio *OpenRouterSyncTask
	OpenRouterSync  *OpenRouterSyncTask
}

// OpenRouterSyncTask separates delivery from financial reconciliation. Headers
// and transport settings are private account provenance, never public Task.Data.
type OpenRouterSyncTask struct {
	CredentialHash    string
	ConfigHash        string
	RoutingHash       string
	CredentialIndex   int
	IdentityConflict  bool
	UpstreamRequestID string
	DispatchPrepared  bool
	ProviderTag       string
	ReservationSource string
	ReserveUSD        float64
	PricingUnknown    bool
	Revision          int
	RequestID         string
	BillingPreference string
	PlannedQuota      int
	LeaseUntil        int64
	Deadline          int64
	MetadataURL       string
	Headers           map[string][]string
	Transport         TransportSettings
	Attempts          int
	NextPoll          int64
	LastHTTPStatus    int
	LastError         string
	ResultReady       bool
	ResultFailed      bool
	ResultMimeType    string
	ResultSize        int64
	ResultSHA256      string
}

// OpenRouterSyncState reads old audio snapshots without rewriting stored JSON.
func (p *TaskPrivateData) OpenRouterSyncState() *OpenRouterSyncTask {
	if p.OpenRouterSync != nil {
		return p.OpenRouterSync
	}
	return p.OpenRouterAudio
}

type TaskReconciliation struct {
	ReservedQuota         int
	ReservationPending    bool
	SubmissionPending     bool
	RefundPending         bool
	Required              bool
	SettlementPending     bool
	MainApplied           bool
	TargetQuota           int
	Finalized             bool
	HasUpstreamCost       bool
	UpstreamCostConfirmed bool
	UpstreamCostUSD       float64
	UpstreamIsBYOK        *bool
	UpstreamCostDetails   string
	QuotaSaturation       string
}

type TaskExecutionSnapshot struct {
	RequestID   string
	RequestPath string
	TaskPlugin  *TaskPluginSnapshot
}

// TaskPluginSnapshot contains credential-free identity only. Plugin source,
// request/response payloads, and channel secrets must never be added here.
type TaskPluginSnapshot struct {
	Key        string
	Name       string
	Version    string
	Author     *TaskPluginAuthorSnapshot
	APIVersion int
	Generation uint64
}

type TaskPluginAuthorSnapshot struct {
	Name string
	URL  string
}

const TaskSettlementOpenRouterCostV1 = "openrouter_cost_v1"
const TaskSettlementOpenRouterCostV2 = "openrouter_cost_v2"

type TaskBillingContext struct {
	SettlementMode       string
	OpenRouterCost       *billingdomain.ProviderBasis
	OpenRouterOrderFacts map[string]any
	ModelPrice           float64
	GroupRatio           float64
	ModelRatio           float64
	OtherRatios          map[string]float64
	OriginModelName      string
	PerCallBilling       bool
	TieredSnapshot       *billingdomain.TieredSnapshot
	BillingFX            *TaskBillingFX
	SubmitGroup          *TaskBillingSubmitGroup
	BillingFXEvidence    json.RawMessage
	BillingFXPresent     bool
	BillingFXComplete    bool
	SubmitGroupEvidence  json.RawMessage
	SubmitGroupPresent   bool
	SubmitGroupComplete  bool
}

// TaskBillingFX is the immutable schema-1 publication basis accepted when a
// modern task was submitted. Factor is always derived from Rate / 100.
type TaskBillingFX struct {
	SchemaVersion      int
	Source             string
	Rate               float64
	PublicationVersion int64
	EffectiveAt        int64
	FetchedAt          int64
}

// TaskBillingSubmitGroup preserves the pure group selection accepted at
// submission. It explains the saved effective GroupRatio without freezing the
// group policy used for a later token-based completion calculation.
type TaskBillingSubmitGroup struct {
	PureRatio       float64
	HasSpecialRatio bool
	SpecialRatio    *float64
}

func (t *Task) GetUpstreamTaskID() string {
	if t.PrivateData.UpstreamTaskID != "" {
		return t.PrivateData.UpstreamTaskID
	}
	return t.TaskID
}

func (t *Task) GetResultURL() string {
	if t.PrivateData.ResultURL != "" {
		return t.PrivateData.ResultURL
	}
	return t.FailReason
}

type TaskSnapshot struct {
	Status         TaskStatus
	Progress       string
	StartTime      int64
	FinishTime     int64
	FailReason     string
	ResultURL      string
	Data           json.RawMessage
	PluginState    json.RawMessage
	PollFailures   int
	Reconciliation TaskReconciliation
}

func (s TaskSnapshot) Equal(other TaskSnapshot) bool {
	return s.Status == other.Status &&
		s.Progress == other.Progress &&
		s.StartTime == other.StartTime &&
		s.FinishTime == other.FinishTime &&
		s.FailReason == other.FailReason &&
		s.ResultURL == other.ResultURL &&
		bytes.Equal(s.Data, other.Data) &&
		bytes.Equal(s.PluginState, other.PluginState) &&
		s.PollFailures == other.PollFailures && s.Reconciliation == other.Reconciliation
}

func (t *Task) Snapshot() TaskSnapshot {
	var reconciliation TaskReconciliation
	if t.PrivateData.Reconciliation != nil {
		reconciliation = *t.PrivateData.Reconciliation
	}
	return TaskSnapshot{
		Status:         t.Status,
		Progress:       t.Progress,
		StartTime:      t.StartTime,
		FinishTime:     t.FinishTime,
		FailReason:     t.FailReason,
		ResultURL:      t.PrivateData.ResultURL,
		Data:           t.Data,
		PluginState:    t.PrivateData.PluginState,
		PollFailures:   t.PrivateData.PollFailures,
		Reconciliation: reconciliation,
	}
}

// Presence and completeness are codec observations, independent of typed zero values.
func (c *TaskBillingContext) HasBillingFX() bool { return c.BillingFXPresent || c.BillingFX != nil }
func (c *TaskBillingContext) HasSubmitGroup() bool {
	return c.SubmitGroupPresent || c.SubmitGroup != nil
}
func (c *TaskBillingContext) HasCompleteBillingFX() bool   { return c.BillingFXComplete }
func (c *TaskBillingContext) HasCompleteSubmitGroup() bool { return c.SubmitGroupComplete }
