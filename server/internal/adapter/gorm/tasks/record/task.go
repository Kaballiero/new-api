package record

import (
	"database/sql/driver"
	"encoding/json"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

type TaskStatus = domain.TaskStatus

const (
	TaskStatusNotStart     = domain.TaskStatusNotStart
	TaskStatusSubmitted    = domain.TaskStatusSubmitted
	TaskStatusQueued       = domain.TaskStatusQueued
	TaskStatusInProgress   = domain.TaskStatusInProgress
	TaskStatusFailure      = domain.TaskStatusFailure
	TaskStatusSuccess      = domain.TaskStatusSuccess
	TaskStatusUnknown      = domain.TaskStatusUnknown
	TaskRefundLegacyCutoff = domain.TaskRefundLegacyCutoff
)

type Task struct {
	ID         int64                 `json:"id" gorm:"primary_key;AUTO_INCREMENT"`
	CreatedAt  int64                 `json:"created_at" gorm:"index"`
	UpdatedAt  int64                 `json:"updated_at"`
	TaskID     string                `json:"task_id" gorm:"type:varchar(191);index"` // Provider song or task identifier; historical records may omit it.
	Platform   constant.TaskPlatform `json:"platform" gorm:"type:varchar(30);index"` // Provider platform.
	UserId     int                   `json:"user_id" gorm:"index"`
	Group      string                `json:"group" gorm:"type:varchar(50)"` // Billing adjustment group.
	ChannelId  int                   `json:"channel_id" gorm:"index"`
	Quota      int                   `json:"quota"`
	Action     string                `json:"action" gorm:"type:varchar(40);index"` // Task action: song, lyrics or description mode.
	Status     TaskStatus            `json:"status" gorm:"type:varchar(20);index"` // Task lifecycle status.
	FailReason string                `json:"fail_reason"`
	SubmitTime int64                 `json:"submit_time" gorm:"index"`
	StartTime  int64                 `json:"start_time" gorm:"index"`
	FinishTime int64                 `json:"finish_time" gorm:"index"`
	Progress   string                `json:"progress" gorm:"type:varchar(20);index"`
	Properties Properties            `json:"properties" gorm:"type:json"`
	Username   string                `json:"username,omitempty" gorm:"-"`
	// Never return private state to clients; it may contain credentials.
	PrivateData TaskPrivateData `json:"-" gorm:"column:private_data;type:json"`
	Data        json.RawMessage `json:"data" gorm:"type:json"`
}

func (t *Task) SetData(data any) {
	b, _ := common.Marshal(data)
	t.Data = json.RawMessage(b)
}

func (t *Task) GetData(v any) error {
	return common.Unmarshal(t.Data, &v)
}

type Properties struct {
	Input             string `json:"input"`
	UpstreamModelName string `json:"upstream_model_name,omitempty"`
	OriginModelName   string `json:"origin_model_name,omitempty"`
}

func (m *Properties) Scan(val any) error {
	bytesValue := jsonScanBytes(val)
	if len(bytesValue) == 0 {
		*m = Properties{}
		return nil
	}
	return common.Unmarshal(bytesValue, m)
}

func (m Properties) Value() (driver.Value, error) {
	if m == (Properties{}) {
		return nil, nil
	}
	// Return string: PostgreSQL simple protocol encodes []byte as bytea,
	// which fails JSON-column writes with SQLSTATE 22P02.
	b, err := common.Marshal(m)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

type TaskPrivateData struct {
	Key            string `json:"key,omitempty"`
	UpstreamTaskID string `json:"upstream_task_id,omitempty"` // Actual upstream task identifier.
	ResultURL      string `json:"result_url,omitempty"`       // Completed result URL, such as a video address.
	// Execution records safe, immutable request provenance. It lives next to
	// other private task state so public task DTOs cannot expose it by accident.
	Execution *TaskExecutionSnapshot `json:"execution,omitempty"`
	// Billing context used by asynchronous refunds and adjustments.
	BillingSource  string              `json:"billing_source,omitempty"`  // "wallet" or "subscription"
	SubscriptionId int                 `json:"subscription_id,omitempty"` // Subscription identifier for refunds.
	TokenId        int                 `json:"token_id,omitempty"`        // API token identifier for quota refunds.
	NodeName       string              `json:"node_name,omitempty"`       // Submitting node for settlement log attribution.
	BillingContext *TaskBillingContext `json:"billing_context,omitempty"` // Billing parameters frozen at submission.
	// ResponsesBackground records that the openai_responses create request
	// asked for background:true. Every task is durable and survives client
	// disconnect regardless; this only echoes the protocol-level request
	// attribute back on retrieval snapshots.
	ResponsesBackground bool `json:"responses_background,omitempty"`
	// PluginState is plugin-owned cross-round data. Unlike Task.Data it is
	// only replaced when a hook explicitly returns state.
	PluginState json.RawMessage `json:"plugin_state,omitempty"`
	// PollFailures counts consecutive unrecognized or transient poll outcomes.
	PollFailures    int                 `json:"poll_failures,omitempty"`
	Reconciliation  *TaskReconciliation `json:"reconciliation,omitempty"`
	OpenRouterAudio *OpenRouterSyncTask `json:"openrouter_audio,omitempty"`
	OpenRouterSync  *OpenRouterSyncTask `json:"openrouter_sync,omitempty"`
}

// OpenRouterSyncTask separates delivery from financial reconciliation. Headers
// and transport settings are private account provenance, never public Task.Data.
type OpenRouterSyncTask struct {
	CredentialHash    string              `json:"credential_hash,omitempty"`
	ConfigHash        string              `json:"config_hash,omitempty"`
	RoutingHash       string              `json:"routing_hash,omitempty"`
	CredentialIndex   int                 `json:"credential_index,omitempty"`
	IdentityConflict  bool                `json:"identity_conflict,omitempty"`
	UpstreamRequestID string              `json:"upstream_request_id,omitempty"`
	DispatchPrepared  bool                `json:"dispatch_prepared,omitempty"`
	ProviderTag       string              `json:"provider_tag,omitempty"`
	ReservationSource string              `json:"reservation_source,omitempty"`
	ReserveUSD        float64             `json:"reserve_usd,omitempty"`
	PricingUnknown    bool                `json:"pricing_unknown,omitempty"`
	Revision          int                 `json:"revision"`
	RequestID         string              `json:"request_id"`
	BillingPreference string              `json:"billing_preference"`
	PlannedQuota      int                 `json:"planned_quota"`
	LeaseUntil        int64               `json:"lease_until"`
	Deadline          int64               `json:"deadline"`
	MetadataURL       string              `json:"metadata_url,omitempty"`
	Headers           map[string][]string `json:"headers,omitempty"`
	Transport         dto.ChannelSettings `json:"transport"`
	Attempts          int                 `json:"attempts"`
	NextPoll          int64               `json:"next_poll,omitempty"`
	LastHTTPStatus    int                 `json:"last_http_status,omitempty"`
	LastError         string              `json:"last_error,omitempty"`
	ResultReady       bool                `json:"result_ready"`
	ResultFailed      bool                `json:"result_failed,omitempty"`
	ResultMimeType    string              `json:"result_mime_type,omitempty"`
	ResultSize        int64               `json:"result_size,omitempty"`
	ResultSHA256      string              `json:"result_sha256,omitempty"`
}

// OpenRouterAudioTask preserves the legacy audio snapshot type.
type OpenRouterAudioTask = OpenRouterSyncTask

// OpenRouterSyncState reads old audio snapshots without rewriting stored JSON.
func (p *TaskPrivateData) OpenRouterSyncState() *OpenRouterSyncTask {
	if p.OpenRouterSync != nil {
		return p.OpenRouterSync
	}
	return p.OpenRouterAudio
}

// TaskReconciliation records uncertainty independently of the public task
// status. Pending financial operations are never automatically replayed.
type TaskReconciliation struct {
	ReservedQuota         int     `json:"reserved_quota"`
	ReservationPending    bool    `json:"reservation_pending,omitempty"`
	SubmissionPending     bool    `json:"submission_pending,omitempty"`
	RefundPending         bool    `json:"refund_pending,omitempty"`
	Required              bool    `json:"required,omitempty"`
	SettlementPending     bool    `json:"settlement_pending,omitempty"`
	MainApplied           bool    `json:"main_applied,omitempty"`
	TargetQuota           int     `json:"target_quota,omitempty"`
	Finalized             bool    `json:"finalized,omitempty"`
	HasUpstreamCost       bool    `json:"has_upstream_cost,omitempty"`
	UpstreamCostConfirmed bool    `json:"upstream_cost_confirmed,omitempty"`
	UpstreamCostUSD       float64 `json:"upstream_cost_usd,omitempty"`
	UpstreamIsBYOK        *bool   `json:"upstream_is_byok,omitempty"`
	UpstreamCostDetails   string  `json:"upstream_cost_details,omitempty"`
	QuotaSaturation       string  `json:"quota_saturation,omitempty"`
}

type TaskExecutionSnapshot struct {
	RequestID   string              `json:"request_id,omitempty"`
	RequestPath string              `json:"request_path,omitempty"`
	TaskPlugin  *TaskPluginSnapshot `json:"task_plugin,omitempty"`
}

// TaskPluginSnapshot contains credential-free identity only. Plugin source,
// request/response payloads, and channel secrets must never be added here.
type TaskPluginSnapshot struct {
	Key        string                    `json:"key"`
	Name       string                    `json:"name"`
	Version    string                    `json:"version"`
	Author     *TaskPluginAuthorSnapshot `json:"author,omitempty"`
	APIVersion int                       `json:"api_version"`
	Generation uint64                    `json:"generation"`
}

type TaskPluginAuthorSnapshot struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

// TaskBillingContext preserves submission parameters for completion repricing.
const TaskSettlementOpenRouterCostV1 = "openrouter_cost_v1"

const TaskSettlementOpenRouterCostV2 = "openrouter_cost_v2"

type TaskBillingContext struct {
	SettlementMode       string                       `json:"settlement_mode,omitempty"`
	OpenRouterCost       *common.OpenRouterCostBasis  `json:"openrouter_cost,omitempty"`
	OpenRouterOrderFacts map[string]any               `json:"openrouter_order_facts,omitempty"`
	ModelPrice           float64                      `json:"model_price,omitempty"`       // Model unit price.
	GroupRatio           float64                      `json:"group_ratio,omitempty"`       // Group ratio.
	ModelRatio           float64                      `json:"model_ratio,omitempty"`       // Model ratio.
	OtherRatios          map[string]float64           `json:"other_ratios,omitempty"`      // Duration, resolution and other multipliers.
	OriginModelName      string                       `json:"origin_model_name,omitempty"` // Original model name used for billing.
	PerCallBilling       bool                         `json:"per_call_billing,omitempty"`  // Per-call pricing skips completion adjustment.
	TieredSnapshot       *billingexpr.BillingSnapshot `json:"tiered_snapshot,omitempty"`
	BillingFX            *TaskBillingFX               `json:"billing_fx,omitempty"`
	SubmitGroup          *TaskBillingSubmitGroup      `json:"submit_group,omitempty"`
	billingFXRaw         json.RawMessage
	submitGroupRaw       json.RawMessage
}

// TaskBillingFX is the immutable schema-1 publication basis accepted when a
// modern task was submitted. Factor is always derived from Rate / 100.
type TaskBillingFX struct {
	SchemaVersion      int     `json:"schema_version"`
	Source             string  `json:"source"`
	Rate               float64 `json:"rate"`
	PublicationVersion int64   `json:"publication_version"`
	EffectiveAt        int64   `json:"effective_at"`
	FetchedAt          int64   `json:"fetched_at"`
}

// TaskBillingSubmitGroup preserves the pure group selection accepted at
// submission. It explains the saved effective GroupRatio without freezing the
// group policy used for a later token-based completion calculation.
type TaskBillingSubmitGroup struct {
	PureRatio       float64  `json:"pure_ratio"`
	HasSpecialRatio bool     `json:"has_special_ratio"`
	SpecialRatio    *float64 `json:"special_ratio,omitempty"`
}

// UnmarshalJSON retains raw member presence for the two modern billing
// members. A typed decode failure is deliberately deferred to the billing
// validator so malformed history cannot be mistaken for absent legacy data.
func (c *TaskBillingContext) UnmarshalJSON(data []byte) error {
	var wire struct {
		SettlementMode       string                       `json:"settlement_mode,omitempty"`
		OpenRouterCost       *common.OpenRouterCostBasis  `json:"openrouter_cost,omitempty"`
		OpenRouterOrderFacts map[string]any               `json:"openrouter_order_facts,omitempty"`
		ModelPrice           float64                      `json:"model_price,omitempty"`
		GroupRatio           float64                      `json:"group_ratio,omitempty"`
		ModelRatio           float64                      `json:"model_ratio,omitempty"`
		OtherRatios          map[string]float64           `json:"other_ratios,omitempty"`
		OriginModelName      string                       `json:"origin_model_name,omitempty"`
		PerCallBilling       bool                         `json:"per_call_billing,omitempty"`
		TieredSnapshot       *billingexpr.BillingSnapshot `json:"tiered_snapshot,omitempty"`
		BillingFX            json.RawMessage              `json:"billing_fx"`
		SubmitGroup          json.RawMessage              `json:"submit_group"`
	}
	if err := common.Unmarshal(data, &wire); err != nil {
		return err
	}
	var members map[string]json.RawMessage
	if err := common.Unmarshal(data, &members); err != nil {
		return err
	}
	billingFXRaw := wire.BillingFX
	if raw, ok := members["billing_fx"]; ok {
		billingFXRaw = raw
	}
	submitGroupRaw := wire.SubmitGroup
	if raw, ok := members["submit_group"]; ok {
		submitGroupRaw = raw
	}
	*c = TaskBillingContext{
		SettlementMode: wire.SettlementMode, OpenRouterCost: wire.OpenRouterCost, OpenRouterOrderFacts: wire.OpenRouterOrderFacts,
		ModelPrice: wire.ModelPrice, GroupRatio: wire.GroupRatio, ModelRatio: wire.ModelRatio,
		OtherRatios: wire.OtherRatios, OriginModelName: wire.OriginModelName, PerCallBilling: wire.PerCallBilling,
		TieredSnapshot: wire.TieredSnapshot, billingFXRaw: append(json.RawMessage(nil), billingFXRaw...),
		submitGroupRaw: append(json.RawMessage(nil), submitGroupRaw...),
	}
	if len(billingFXRaw) > 0 && string(billingFXRaw) != "null" {
		var fx TaskBillingFX
		if common.Unmarshal(billingFXRaw, &fx) == nil {
			c.BillingFX = &fx
		}
	}
	if len(submitGroupRaw) > 0 && string(submitGroupRaw) != "null" {
		var group TaskBillingSubmitGroup
		if common.Unmarshal(submitGroupRaw, &group) == nil {
			c.SubmitGroup = &group
		}
	}
	return nil
}

func (c *TaskBillingContext) HasBillingFX() bool {
	return c != nil && (len(c.billingFXRaw) > 0 || c.BillingFX != nil)
}

func (c *TaskBillingContext) HasSubmitGroup() bool {
	return c != nil && (len(c.submitGroupRaw) > 0 || c.SubmitGroup != nil)
}

func (c *TaskBillingContext) HasCompleteBillingFX() bool {
	if c == nil || len(c.billingFXRaw) == 0 {
		return true
	}
	var fields map[string]json.RawMessage
	if common.Unmarshal(c.billingFXRaw, &fields) != nil {
		return false
	}
	for _, key := range []string{"schema_version", "source", "rate", "publication_version", "effective_at", "fetched_at"} {
		if value, ok := fields[key]; !ok || string(value) == "null" {
			return false
		}
	}
	return true
}

func (c *TaskBillingContext) HasCompleteSubmitGroup() bool {
	if c == nil || len(c.submitGroupRaw) == 0 {
		return true
	}
	var fields map[string]json.RawMessage
	if common.Unmarshal(c.submitGroupRaw, &fields) != nil {
		return false
	}
	pure, hasPure := fields["pure_ratio"]
	hasSpecial, hasHasSpecial := fields["has_special_ratio"]
	if !hasPure || !hasHasSpecial || string(pure) == "null" || string(hasSpecial) == "null" {
		return false
	}
	if c.SubmitGroup != nil && c.SubmitGroup.HasSpecialRatio {
		special, ok := fields["special_ratio"]
		return ok && string(special) != "null"
	}
	_, hasSpecialValue := fields["special_ratio"]
	return !hasSpecialValue
}

func (c TaskBillingContext) MarshalJSON() ([]byte, error) {
	var billingFX, submitGroup json.RawMessage
	if len(c.billingFXRaw) > 0 {
		billingFX = c.billingFXRaw
	} else if c.BillingFX != nil {
		data, err := common.Marshal(c.BillingFX)
		if err != nil {
			return nil, err
		}
		billingFX = data
	}
	if len(c.submitGroupRaw) > 0 {
		submitGroup = c.submitGroupRaw
	} else if c.SubmitGroup != nil {
		data, err := common.Marshal(c.SubmitGroup)
		if err != nil {
			return nil, err
		}
		submitGroup = data
	}
	return common.Marshal(struct {
		SettlementMode       string                       `json:"settlement_mode,omitempty"`
		OpenRouterCost       *common.OpenRouterCostBasis  `json:"openrouter_cost,omitempty"`
		OpenRouterOrderFacts map[string]any               `json:"openrouter_order_facts,omitempty"`
		ModelPrice           float64                      `json:"model_price,omitempty"`
		GroupRatio           float64                      `json:"group_ratio,omitempty"`
		ModelRatio           float64                      `json:"model_ratio,omitempty"`
		OtherRatios          map[string]float64           `json:"other_ratios,omitempty"`
		OriginModelName      string                       `json:"origin_model_name,omitempty"`
		PerCallBilling       bool                         `json:"per_call_billing,omitempty"`
		TieredSnapshot       *billingexpr.BillingSnapshot `json:"tiered_snapshot,omitempty"`
		BillingFX            json.RawMessage              `json:"billing_fx,omitempty"`
		SubmitGroup          json.RawMessage              `json:"submit_group,omitempty"`
	}{c.SettlementMode, c.OpenRouterCost, c.OpenRouterOrderFacts, c.ModelPrice, c.GroupRatio, c.ModelRatio, c.OtherRatios, c.OriginModelName, c.PerCallBilling, c.TieredSnapshot, billingFX, submitGroup})
}

// GetUpstreamTaskID returns the provider identifier.
// Historical records use TaskID directly when no upstream identifier exists.
func (t *Task) GetUpstreamTaskID() string {
	if t.PrivateData.UpstreamTaskID != "" {
		return t.PrivateData.UpstreamTaskID
	}
	return t.TaskID
}

// GetResultURL returns the completed task result address.
// Historical records fall back to FailReason instead of private ResultURL.
func (t *Task) GetResultURL() string {
	if t.PrivateData.ResultURL != "" {
		return t.PrivateData.ResultURL
	}
	return t.FailReason
}

func (p *TaskPrivateData) Scan(val any) error {
	bytesValue := jsonScanBytes(val)
	if len(bytesValue) == 0 {
		return nil
	}
	return common.Unmarshal(bytesValue, p)
}

func (p TaskPrivateData) Value() (driver.Value, error) {
	if p.Key == "" && p.UpstreamTaskID == "" && p.ResultURL == "" &&
		p.Execution == nil && p.BillingSource == "" && p.SubscriptionId == 0 &&
		p.TokenId == 0 && p.NodeName == "" && p.BillingContext == nil &&
		!p.ResponsesBackground && len(p.PluginState) == 0 && p.PollFailures == 0 && p.Reconciliation == nil {
		return nil, nil
	}
	// Like Properties.Value, string avoids PostgreSQL bytea encoding.
	b, err := common.Marshal(p)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// SyncTaskQueryParams describes the existing task search filters.
type SyncTaskQueryParams struct {
	Platform       constant.TaskPlatform
	ChannelID      string
	TaskID         string
	UserID         string
	Action         string
	Status         string
	StartTimestamp int64
	EndTimestamp   int64
	UserIDs        []int
}
