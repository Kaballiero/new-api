package get_affiliate_code

// AffiliateFailure различает ошибку чтения и прежнюю особую ошибку сохранения.
type AffiliateFailure struct {
	Err    error
	Saving bool
}

type Input struct{ ID int }
