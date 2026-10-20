package transfer_affiliate_quota

type AffiliateTransferFailure struct {
	Stage AffiliateTransferStage
	Err   error
}

// AffiliateBalances содержит суммы для перевода приглашённой квоты в обычную.
type AffiliateBalances struct{ Quota, AffiliateQuota int }

type TransferAffQuotaRequest struct {
	Quota int `json:"quota" binding:"required"`
}
