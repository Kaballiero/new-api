package transfer_affiliate_quota

import (
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
)

// AffiliateAccountLoader загружает аккаунт до чтения запроса перевода.
type AffiliateAccountLoader interface {
	LoadAffiliateAccount(id int) (AffiliateAccountTransfer, error)
}

// AffiliateAccountTransfer сохраняет текущую точку диспетчеризации перевода аккаунта.
type AffiliateAccountTransfer interface{ TransferQuota(quota int) error }

type AffiliateQuotaInput interface{ ReadQuota() (int, error) }

type AffiliateTransferStage uint8

const (
	AffiliateTransferLoad AffiliateTransferStage = iota
	AffiliateTransferInput
	AffiliateTransferDispatch
)

// ExecuteAffiliateTransfer сохраняет порядок загрузки, чтения ввода и вызова перевода.
func ExecuteAffiliateTransfer(id int, loader AffiliateAccountLoader, input AffiliateQuotaInput) *AffiliateTransferFailure {
	account, err := loader.LoadAffiliateAccount(id)
	if err != nil {
		return &AffiliateTransferFailure{Stage: AffiliateTransferLoad, Err: err}
	}
	quota, err := input.ReadQuota()
	if err != nil {
		return &AffiliateTransferFailure{Stage: AffiliateTransferInput, Err: err}
	}
	if err := account.TransferQuota(quota); err != nil {
		return &AffiliateTransferFailure{Stage: AffiliateTransferDispatch, Err: err}
	}
	return nil
}

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

// TransferAffiliateQuota определяет финансовые проверки и порядок транзакции перевода.
func TransferAffiliateQuota(quota int, operation AffiliateTransfer, display AffiliateQuotaDisplay) error {
	if float64(quota) < display.MinimumQuota() {
		return fmt.Errorf("转移额度最小为%s！", display.FormatQuota(common.QuotaFromFloat(display.MinimumQuota())))
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
		return errors.New("邀请额度不足！")
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
func (u Usecase) Execute(id int, input AffiliateQuotaInput) *AffiliateTransferFailure {
	return ExecuteAffiliateTransfer(id, u.loader, input)
}
