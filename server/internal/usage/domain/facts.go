package domain

import billingdomain "github.com/QuantumNous/new-api/internal/billing/domain"

type UsageRecord struct {
	UserID, ChannelID, TokenID  int
	ModelName, TokenName, Group string
	Quota                       int
	PromptTokens                int
	CompletionTokens            int
	UseTimeSeconds              int
	IsStream                    bool
}
type Prices struct {
	ModelRatio      float64
	GroupRatio      float64
	CompletionRatio float64
	CacheRatio      float64
	ModelPrice      float64
	UserGroupRatio  float64
	CacheTokens     int
}
type GroupRatios struct {
	GroupRatio, GroupSpecialRatio float64
	HasSpecialRatio               bool
}
type PriceFacts struct {
	GroupRatioInfo GroupRatios
	EffectiveRatio float64
}
type FX struct {
	Rate, Factor       float64
	Source             string
	PublicationVersion int64
	EffectiveAt        int64
	FetchedAt          int64
}
type OpenRouterCostState struct {
	Basis       billingdomain.ProviderBasis
	CostUSD     float64
	Final, Sent bool

	IsBYOK                                  *bool
	UnknownReason, CostSource, GenerationID string
	ConfiguredReserveUSD                    *float64
	ApproximateReserveUSD                   *float64
	AddOnReserveUSD                         *float64
	ApproximateResearchReserveUSD           *float64
	ConfiguredReserveModel                  string
	ConfiguredReserveExprHash               string
	ApproximateReserveModel                 string
	AddOnReserveModel                       string
}
type NativeImage struct {
	EstimatedCostUSD float64
	ActualCostUSD    *float64
	ProviderTag      string
	OutcomeUnknown   bool
}
type Diagnostic struct {
	Code     string
	Path     string
	Message  string
	Severity string
	From     string
	To       string
}
type Stream struct {
	Normal              bool
	EndReason, EndError string
	ErrorCount          int
	Errors              []string
}
type Tiered struct {
	ExprString           string
	EstimatedTier        string
	EstimatedBillingUnit string
	EstimatedFixedPrice  *float64
	Actual               *TieredResult
}
type TieredResult struct {
	MatchedTier, BillingUnit string
	FixedPrice               *float64
	RequestRules             []billingdomain.RequestRuleTrace
}
type RequestFacts struct {
	ContextPresent, URLPresent bool
	Path, RequestURLPath       string
	UseChannel                 []string
	SystemPromptOverwritten    bool
	MultiKey                   bool
	LocalCount                 bool
	MultiKeyIndex              int

	OriginModelName                       string
	BillingModelName                      string
	ReasoningEffort                       string
	UpstreamModelName                     string
	IsModelMapped, IsStream               bool
	FirstResponseMilliseconds             float64
	Formats                               []string
	FinalClaude                           bool
	Diagnostics                           []Diagnostic
	DiagnosticsTruncated                  bool
	ParamOverrideAudit                    []string
	Stream                                *Stream
	PriceData                             PriceFacts
	FX                                    FX
	OpenRouterCost                        *OpenRouterCostState
	NativeImage                           *NativeImage
	HasSyncTask                           bool
	FinalPreConsumedQuota                 int
	BillingSource, BillingPreference      string
	SubscriptionId, SubscriptionPlanId    int
	SubscriptionPreConsumed               int64
	SubscriptionPostDelta                 int64
	SubscriptionAmountUsedAfterPreConsume int64
	SubscriptionAmountTotal               int64
	SubscriptionPlanTitle                 string
	Tiered                                *Tiered
	// Clamp is a billing fact; its persisted audit shape belongs to the serializer.
	Clamp        *billingdomain.Clamp
	ClampWarning string
}
type TextFacts struct {
	Claude                      bool
	UsagePath, RejectReason     string
	ImageTokens                 int
	ImageRatio, AudioInputPrice float64
	AudioTokens                 int
	Tools                       []billingdomain.ToolSurchargeItem
	CacheCreationTokens         int
	CacheCreationTokens5m       int
	CacheCreationTokens1h       int
	CacheWriteTokens            int
	CacheCreationRatio          float64
	CacheCreationRatio5m        float64
	CacheCreationRatio1h        float64
	InputTokensTotal            int
	Outcome                     *billingdomain.Outcome
	TieredApplied               bool
}
type AudioFacts struct {
	Realtime                         bool
	InputAudio                       int
	OutputAudio                      int
	InputText                        int
	OutputText                       int
	AudioRatio, AudioCompletionRatio float64
	CacheDiscountUnavailable         bool
	CacheDiscountReason              string
}
type ConsumptionKind int

const (
	TextConsumption ConsumptionKind = iota
	AudioConsumption
	ChannelTestConsumption
)

type Consumption struct {
	Kind            ConsumptionKind
	Notes           []Note
	Prices          Prices
	Record          UsageRecord
	ProbeLabel      string
	RenderedContent string
	Request         RequestFacts
	Text            *TextFacts
	Audio           *AudioFacts
	DeliveryFailure string
}
type Failure struct {
	DeliveryReason                string
	Record                        UsageRecord
	Content, ErrorType, ErrorCode string
	StatusCode                    int
	Request                       RequestFacts
	Consumption                   *Consumption
	Plugin                        *Plugin
}
type Plugin struct {
	Key        string
	Name       string
	Version    string
	AuthorName string
	AuthorURL  string
	APIVersion int
	Generation uint64
}
