package transfer_affiliate_quota

import "github.com/QuantumNous/new-api/internal/accounts/domain"

// AffiliateAccountLoader loads the account before the transfer request is read.
type AffiliateAccountLoader interface {
	LoadAffiliateAccount(id int) (AffiliateAccountTransfer, error)
}

// AffiliateAccountTransfer is the dispatch point of the account transfer.
type AffiliateAccountTransfer interface{ TransferQuota(quota int) error }

type AffiliateQuotaInput interface{ ReadQuota() (int, error) }

type AffiliateTransfer interface {
	Begin() (AffiliateTransferTx, error)
}

type AffiliateTransferTx interface {
	LockBalances() (AffiliateBalances, error)
	SaveBalances(AffiliateBalances) error
	Commit() error
	Rollback() error
}

type AffiliateQuotaDisplay interface {
	MinimumQuota() float64
	FormatQuota(int) string
}

// QuotaMath converts the displayed minimum using the existing saturating policy.
type QuotaMath func(float64) int

// TransferAffiliateQuota owns the financial checks and the transaction order
// of moving affiliate quota into the regular quota.
func TransferAffiliateQuota(quota int, operation AffiliateTransfer, display AffiliateQuotaDisplay, math QuotaMath) error {
	if float64(quota) < display.MinimumQuota() {
		return &domain.AmountError{Err: domain.ErrTransferBelowMinimum, Amount: display.FormatQuota(math(display.MinimumQuota()))}
	}
	tx, err := operation.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	balances, err := tx.LockBalances()
	if err != nil {
		return err
	}
	if balances.AffiliateQuota < quota {
		return domain.ErrInviteQuotaInsufficient
	}
	balances.AffiliateQuota -= quota
	balances.Quota += quota
	if err := tx.SaveBalances(balances); err != nil {
		return err
	}
	return tx.Commit()
}

type Usecase struct{ loader AffiliateAccountLoader }

func NewUsecase(loader AffiliateAccountLoader) Usecase { return Usecase{loader: loader} }

// Execute keeps the order of loading the account, reading the input and
// dispatching the transfer. Load and input failures are returned unclassified;
// a rejected transfer is wrapped with domain.AffiliateTransferFailed.
func (u Usecase) Execute(id int, input AffiliateQuotaInput) error {
	account, err := u.loader.LoadAffiliateAccount(id)
	if err != nil {
		return err
	}
	quota, err := input.ReadQuota()
	if err != nil {
		return err
	}
	if err := account.TransferQuota(quota); err != nil {
		return domain.AffiliateTransferFailed(err)
	}
	return nil
}
