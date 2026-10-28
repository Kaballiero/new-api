package get_affiliate_code

type Usecase struct {
	affiliate    AffiliateReader
	randomString func(int) string
}

func NewUsecase(reader AffiliateReader, randomString func(int) string) Usecase {
	return Usecase{affiliate: reader, randomString: randomString}
}

// AffiliateAccount keeps the previous update lifecycle of the account.
type AffiliateAccount interface {
	AffiliateCode() string
	SaveAffiliateCode(code string) error
}

type AffiliateReader interface {
	AffiliateAccount(id int) (AffiliateAccount, error)
}

// Execute returns the affiliate code and creates it on first use; load and
// save failures are returned unclassified.
func (queries Usecase) Execute(input Input) (string, error) {
	account, err := queries.affiliate.AffiliateAccount(input.ID)
	if err != nil {
		return "", err
	}
	if account.AffiliateCode() == "" {
		if err := account.SaveAffiliateCode(queries.randomString(4)); err != nil {
			return "", err
		}
	}
	return account.AffiliateCode(), nil
}
