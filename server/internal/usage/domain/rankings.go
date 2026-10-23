package domain

type RankingQuotaTotal struct {
	ModelName   string
	TotalTokens int64
}

type RankingQuotaBucket struct {
	ModelName string
	Bucket    int64
	Tokens    int64
}

type RankingVendor struct {
	ID         int
	Name, Icon string
}

type RankingPrice struct {
	ModelName string
	VendorID  int
	OwnerBy   string
}
