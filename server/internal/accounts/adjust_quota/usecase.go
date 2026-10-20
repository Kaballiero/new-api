package adjust_quota

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/accounts"
	"github.com/shopspring/decimal"
)

type QuotaAdjustmentStore interface {
	Transaction(func(QuotaAdjustmentTx) error) error
}
type QuotaAdjustmentTx interface {
	LockAccount(int) (QuotaAccount, error)
	UpdateQuota(userID, after int) error
}
type QuotaCache interface {
	Increment(userID int, delta int64) error
}
type QuotaCacheFailureLog interface{ AdjustmentCacheFailed(userID int, cause error) }

// QuotaAdjustmentErrors читает независимые текущие ошибки хранилища в точке отказа.
type QuotaAdjustmentErrors interface {
	Invalid() error
	Permission() error
	Limit() error
}

type Usecase struct {
	Store      QuotaAdjustmentStore
	Cache      QuotaCache
	FailureLog QuotaCacheFailureLog
	Errors     QuotaAdjustmentErrors
	RootRole   int
}

func (scenario Usecase) Execute(userID, operatorRole int, mode string, value int) (*accounts.UserQuotaAdjustment, error) {
	if userID <= 0 || (mode != "add" && mode != "subtract" && mode != "override") {
		return nil, scenario.Errors.Invalid()
	}
	if mode != "override" && value <= 0 {
		return nil, scenario.Errors.Invalid()
	}
	if value > common.MaxWalletQuota || value < -common.MaxWalletQuota {
		return nil, scenario.Errors.Limit()
	}

	var adjustment accounts.UserQuotaAdjustment
	err := scenario.Store.Transaction(func(tx QuotaAdjustmentTx) error {
		user, err := tx.LockAccount(userID)
		if err != nil {
			return err
		}
		if operatorRole != scenario.RootRole && operatorRole <= user.Role {
			return scenario.Errors.Permission()
		}
		if user.Quota > common.MaxWalletQuota || user.Quota < -common.MaxWalletQuota {
			return scenario.Errors.Limit()
		}
		quota := decimal.NewFromInt(int64(value))
		switch mode {
		case "add":
			quota = decimal.NewFromInt(int64(user.Quota)).Add(quota)
		case "subtract":
			quota = decimal.NewFromInt(int64(user.Quota)).Sub(quota)
		}
		after, err := common.WalletQuotaFromDecimalStrict(quota)
		if err != nil {
			return scenario.Errors.Limit()
		}
		// Неизменившееся установленное значение считается успехом, в том числе в MySQL,
		// где RowsAffected может считать только фактически изменённые строки.
		if after != user.Quota {
			if err := tx.UpdateQuota(userID, after); err != nil {
				return err
			}
		}
		adjustment = accounts.UserQuotaAdjustment{UserID: user.ID, Username: user.Username, Role: user.Role, Status: user.Status, Before: user.Quota, After: after}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Применяем лишь зафиксированную разницу, сохраняя незавершённые резервы.
	// Оба баланса ограничены сверху, поэтому разница помещается в int64.
	delta := int64(adjustment.After) - int64(adjustment.Before)
	if delta != 0 {
		if err := scenario.Cache.Increment(userID, delta); err != nil {
			scenario.FailureLog.AdjustmentCacheFailed(userID, err)
		}
	}
	return &adjustment, nil
}

func NewUsecase(store QuotaAdjustmentStore, cache QuotaCache, failureLog QuotaCacheFailureLog, errors QuotaAdjustmentErrors, rootRole int) Usecase {
	return Usecase{Store: store, Cache: cache, FailureLog: failureLog, Errors: errors, RootRole: rootRole}
}
