package domain

type Job struct {
	ID                int64  `json:"-"`
	PublicID          string `json:"id"`
	UserID            int    `json:"-"`
	TokenID           int    `json:"-"`
	IdempotencyKey    string `json:"-"`
	RequestHash       string `json:"-"`
	ChannelID         int    `json:"-"`
	ChannelType       int    `json:"-"`
	CredentialIndex   int    `json:"-"`
	CredentialHash    string `json:"-"`
	ChannelConfigHash string `json:"-"`
	Model             string `json:"model"`
	UpstreamModel     string `json:"-"`
	UsingGroup        string `json:"-"`
	Endpoint          string `json:"endpoint"`
	Status            string `json:"status"`
	PurchaseUSD       string `json:"-"`
	PurchaseEvidence  string `json:"-"`
	PurchaseStatus    string `json:"purchase_status"`
	BillingStatus     string `json:"billing_status"`
	SubmitState       string `json:"-"`
	UpstreamID        string `json:"-"`
	InputFileID       string `json:"-"`
	OutputFileID      string `json:"-"`
	ErrorFileID       string `json:"-"`
	BillingSnapshot   string `json:"-"`
	UpstreamUsage     string `json:"-"`
	FinancialDetails  string `json:"-"`
	LastError         string `json:"-"`
	RequestCount      int    `json:"request_count"`
	ResultCount       int    `json:"result_count"`
	SettledCount      int    `json:"settled_count"`
	ReservedQuota     int64  `json:"reserved_quota"`
	ChargedQuota      int64  `json:"charged_quota"`
	CreatedAt         int64  `json:"created_at"`
	UpdatedAt         int64  `json:"updated_at"`
	ExpiresAt         int64  `json:"expires_at"`
	DeletedAt         int64  `json:"-"`
	CleanupDone       bool   `json:"-"`
	LeaseUntil        int64  `json:"-"`
	LeaseOwner        string `json:"-"`
}

type Failure struct {
	Version    int    `json:"version"`
	Phase      string `json:"phase"`
	Code       string `json:"code"`
	Category   string `json:"category,omitempty"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"http_status,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
}
