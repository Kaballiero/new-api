package credit_getapi_quota

type Input struct {
	UserID       int
	OperationKey string
	Quota        int
}
type GetAPIQuotaCreditResult struct {
	Applied        bool `json:"applied"`
	AlreadyApplied bool `json:"alreadyApplied"`
	Quota          int  `json:"quota"`
}
