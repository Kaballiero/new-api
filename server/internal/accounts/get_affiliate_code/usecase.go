package get_affiliate_code

type Usecase struct {
	Affiliate    AffiliateReader
	RandomString func(int) string
}

func NewUsecase(reader AffiliateReader, randomString func(int) string) Usecase {
	return Usecase{Affiliate: reader, RandomString: randomString}
}

// AffiliateAccount сохраняет прежний жизненный цикл изменения аккаунта.
type AffiliateAccount interface {
	AffiliateCode() string
	SaveAffiliateCode(code string) error
}

type AffiliateReader interface {
	AffiliateAccount(id int) (AffiliateAccount, error)
}

func (queries Usecase) Execute(input Input) (string, *AffiliateFailure) {
	account, err := queries.Affiliate.AffiliateAccount(input.ID)
	if err != nil {
		return "", &AffiliateFailure{Err: err}
	}
	if account.AffiliateCode() == "" {
		if err := account.SaveAffiliateCode(queries.RandomString(4)); err != nil {
			return "", &AffiliateFailure{Err: err, Saving: true}
		}
	}
	return account.AffiliateCode(), nil
}
