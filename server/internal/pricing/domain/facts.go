package domain

import "encoding/json"

// Pricing facts contain no runtime engine or transport dependencies.
type SourceReference struct {
	ID   string `json:"id"`
	Hash string `json:"hash"`
}
type SourceManifest struct {
	SchemaVersion        int               `json:"schemaVersion"`
	NormalizationVersion string            `json:"normalizationVersion"`
	Revision             string            `json:"revision"`
	SourceRevision       string            `json:"sourceRevision"`
	FetchedAt            string            `json:"fetchedAt"`
	Models               []SourceReference `json:"models"`
}
type SourceCoverage struct {
	Offers          string   `json:"offers"`
	Normalization   string   `json:"normalization"`
	Reasons         []string `json:"reasons"`
	ExpectedSources []string `json:"expectedSources"`
	MissingSources  []string `json:"missingSources"`
}
type SourceProvider struct {
	ID   *string `json:"id"`
	Name *string `json:"name"`
	Tag  *string `json:"tag"`
}
type SourceAlias struct {
	TargetID string `json:"targetId"`
}
type SourceAuthor struct {
	Namespace string `json:"namespace"`
}
type SourceModalities struct {
	Input  []string `json:"input"`
	Output []string `json:"output"`
}
type SourceDocument struct {
	SchemaVersion int              `json:"schemaVersion"`
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	Description   *string          `json:"description"`
	Author        *SourceAuthor    `json:"author"`
	Type          string           `json:"type"`
	Modalities    SourceModalities `json:"modalities"`
	Metadata      json.RawMessage  `json:"metadata"`
	Capabilities  json.RawMessage  `json:"capabilities"`
	Limits        json.RawMessage  `json:"limits"`
	Alias         *SourceAlias     `json:"alias"`
	Offers        []SourceOffer    `json:"offers"`
	Coverage      SourceCoverage   `json:"coverage"`
	Limitations   []string         `json:"limitations"`
	Provenance    json.RawMessage  `json:"provenance"`
}
type SourceEligibility struct {
	Eligible bool     `json:"eligible"`
	Reasons  []string `json:"reasons"`
}
type SourceOffer struct {
	ID           string             `json:"id"`
	Scope        string             `json:"scope"`
	Identity     json.RawMessage    `json:"identity"`
	Provider     *SourceProvider    `json:"provider"`
	Operation    *string            `json:"operation"`
	Mode         *string            `json:"mode"`
	Variant      json.RawMessage    `json:"variant"`
	Capabilities json.RawMessage    `json:"capabilities"`
	Limits       json.RawMessage    `json:"limits"`
	Eligibility  *SourceEligibility `json:"eligibility"`
	Components   []SourceRate       `json:"components"`
	Coverage     SourceCoverage     `json:"coverage"`
	Limitations  []string           `json:"limitations"`
	Facts        json.RawMessage    `json:"facts"`
}
type SourceRate struct {
	Parameter   string          `json:"parameter"`
	ComponentID string          `json:"componentId"`
	Billable    *string         `json:"billable"`
	Direction   *string         `json:"direction"`
	Modality    *string         `json:"modality"`
	AmountUSD   *string         `json:"amountUSD"`
	Unit        string          `json:"unit"`
	Quantity    string          `json:"quantity"`
	Variant     json.RawMessage `json:"variant"`
	Conditions  json.RawMessage `json:"conditions"`
	Source      struct {
		Amount   *string `json:"amount"`
		Currency *string `json:"currency"`
		Unit     *string `json:"unit"`
		Key      string  `json:"key"`
	} `json:"source"`
	Provenance    json.RawMessage `json:"provenance"`
	Normalization struct {
		Comparable      bool     `json:"comparable"`
		Effective       bool     `json:"effective"`
		Reasons         []string `json:"reasons"`
		DiscountBasis   string   `json:"discountBasis"`
		AppliedDiscount *string  `json:"appliedDiscount"`
	} `json:"normalization"`
}
type TariffPrice struct {
	Component string   `json:"component"`
	Unit      string   `json:"unit"`
	AmountUSD *float64 `json:"amount_usd"`
}
type TariffUsageCondition struct {
	Field   string `json:"field"`
	Value   string `json:"value"`
	Negated bool   `json:"negated,omitempty"`
}
type TariffOffer struct {
	PublicUsageConditions []TariffUsageCondition `json:"display_usage_conditions,omitempty"`
	PublicAlternatives    []TariffOffer          `json:"display_alternatives,omitempty"`
	PublicConditions      []ExpressionCondition  `json:"display_conditions,omitempty"`
	PublicConditional     bool                   `json:"display_conditional,omitempty"`
	ID                    string                 `json:"id"`
	Operation             string                 `json:"operation"`
	Mode                  string                 `json:"mode"`
	MainComponent         string                 `json:"main_component"`
	Prices                []TariffPrice          `json:"prices"`
}
type TariffCatalog struct {
	FetchedAt string                   `json:"fetched_at"`
	Models    map[string][]TariffOffer `json:"models"`
}
type VideoPriceBasis struct {
	Unit       string `json:"unit"`
	UsageField string `json:"usageField,omitempty"`
}
type TariffUsageExample struct {
	Label string         `json:"label"`
	Facts map[string]any `json:"facts"`
}
type TariffLocalizedText = json.RawMessage
type TariffUsageField struct {
	Type        string                         `json:"type,omitempty"`
	Unit        string                         `json:"unit,omitempty"`
	Enum        []string                       `json:"enum,omitempty"`
	Description map[string]string              `json:"description,omitempty"`
	EnumLabels  map[string]TariffLocalizedText `json:"enumLabels,omitempty"`
}
type ExpressionPrice struct {
	Component string
	Unit      string
	AmountUSD float64
}
type ExpressionPriceTier struct {
	Prices     []ExpressionPrice
	Conditions []ExpressionCondition
}
type ExpressionCondition struct {
	Var      string
	Arg      string
	Op       string
	Value    float64
	Negated  bool
	Operands []ExpressionCondition
}
type ExpressionTier struct {
	Label        string
	Conditions   []ExpressionCondition
	Coefficients map[string]float64
	Linear       bool
}
