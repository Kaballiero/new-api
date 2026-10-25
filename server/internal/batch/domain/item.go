package domain

type Item struct {
	ID               int64  `json:"-"`
	BatchID          int64  `json:"-"`
	CustomID         string `json:"custom_id"`
	ProviderCustomID string `json:"-"`
	CustomIDHash     string `json:"-"`
	Ordinal          int    `json:"-"`
	Body             string `json:"-"`
	UpstreamBody     string `json:"-"`
	Result           string `json:"-"`
	ResultHash       string `json:"-"`
	PurchaseStatus   string `json:"purchase_status"`
	BillingStatus    string `json:"billing_status"`
	ReservedQuota    int64  `json:"reserved_quota"`
	ChargedQuota     int64  `json:"charged_quota"`
	CachePending     bool   `json:"-"`
	FinancialDetails string `json:"-"`
}
