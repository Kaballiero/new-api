package transfer_affiliate_quota

// AffiliateBalances holds the balances of an affiliate quota transfer.
type AffiliateBalances struct{ Quota, AffiliateQuota int }

type TransferAffQuotaRequest struct {
	Quota int `json:"quota" binding:"required"`
}
