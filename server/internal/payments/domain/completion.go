package domain

import (
	"errors"

	"github.com/shopspring/decimal"
)

// QuotaMath preserves the existing strict wallet conversion and ceiling.
type QuotaMath interface {
	Strict(quota decimal.Decimal) (int, error)
	Maximum() int
}

// CompletionEffects synchronizes cache and records the receipt only after the database commit.
type CompletionEffects interface {
	Sync(userID int, quota int, operation string)
	Receipt(order Order, quota int, callerIP string, provider string)
	Failed(operation string, err error)
}

type Completion struct {
	Transactions Transactions
	Math         QuotaMath
	Effects      CompletionEffects
	Now          func() int64
	QuotaPerUnit func() float64
}

type CompletionInput struct {
	TradeNo, Provider, Method, Customer, Email, CallerIP string
	Manual                                               bool
}

// Complete owns provider-specific quota formulas, duplicate handling and the atomic order/wallet boundary.
func (c Completion) Complete(in CompletionInput) (bool, error) {
	if in.TradeNo == "" {
		if in.Manual {
			return false, ErrTradeNo
		}
		return false, ErrPaymentTradeNo
	}
	var order Order
	var quota int
	var alreadyDone bool
	err := c.Transactions.Transaction(
		func(tx Tx) error {
			var err error
			found, err := tx.FindLocked(in.TradeNo)
			if err != nil {
				if in.Provider == PaymentProviderEpay && !in.Manual {
					return ErrNotFound
				}
				return ErrOrderMissing
			}
			if !in.Manual && found.PaymentProvider != in.Provider {
				return ErrProviderMismatch
			}
			if found.Status == StatusSuccess && (in.Manual || in.Provider == PaymentProviderEpay || in.Provider == PaymentProviderWaffo || in.Provider == PaymentProviderWaffoPancake) {
				alreadyDone = true
				// Manual completion historically keeps zero receipt facts on repeats.
				if !in.Manual {
					order = found
				}
				return nil
			}
			if found.Status != StatusPending {
				if in.Manual {
					return ErrManualStatus
				}
				if in.Provider == PaymentProviderEpay {
					return ErrNotPending
				}
				return ErrOrderStatus
			}
			order = found
			if in.Provider == PaymentProviderEpay && in.Method != "" && order.PaymentMethod != in.Method {
				order.PaymentMethod = in.Method
			}
			// Stripe and Creem save before calculating quota; the other paths calculate first.
			saveFirst := !in.Manual && (in.Provider == PaymentProviderStripe || in.Provider == PaymentProviderCreem)
			if saveFirst {
				order.CompleteTime = c.Now()
				order.Status = StatusSuccess
				if err := tx.Save(&order); err != nil {
					return err
				}
			}
			var value decimal.Decimal
			switch {
			case order.PaymentProvider == PaymentProviderStripe:
				value = decimal.NewFromFloat(order.Money).Mul(decimal.NewFromFloat(c.QuotaPerUnit()))
			case in.Provider == PaymentProviderCreem && !in.Manual:
				value = decimal.NewFromInt(order.Amount)
			default:
				value = decimal.NewFromInt(order.Amount).Mul(decimal.NewFromFloat(c.QuotaPerUnit()))
			}

			quota, err = c.Math.Strict(value)
			if err != nil || quota <= 0 {
				return ErrInvalidQuota
			}
			if !saveFirst {
				order.CompleteTime = c.Now()
				order.Status = StatusSuccess
				if err := tx.Save(&order); err != nil {
					return err
				}
			}
			var profile CreditProfile
			if !in.Manual && in.Provider == PaymentProviderStripe {
				profile.StripeCustomer = &in.Customer
			}
			if !in.Manual && in.Provider == PaymentProviderCreem {

				if in.Email != "" {
					email, err := tx.Email(order.UserID)
					if err != nil {
						return err
					}
					if email == "" {
						profile.Email = &in.Email
					}
				}
			}
			return tx.Credit(order.UserID, quota, profile)
		},
	)
	if err != nil {
		if in.Manual {
			return false, err
		}
		if in.Provider == PaymentProviderEpay {
			if !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrProviderMismatch) && !errors.Is(err, ErrNotPending) {
				c.Effects.Failed("epay topup", err)
			}
			return false, err
		}
		operation := in.Provider + " topup"
		if in.Provider == PaymentProviderStripe {
			operation = "topup"
		}
		if in.Provider == PaymentProviderWaffoPancake {
			operation = "waffo pancake topup"
		}
		c.Effects.Failed(operation, err)
		return false, ErrRecharge
	}
	if alreadyDone && in.Provider == PaymentProviderEpay && !in.Manual {
		return true, nil
	}
	operation := in.Provider + " topup"
	if in.Provider == PaymentProviderWaffoPancake {
		operation = "waffo pancake topup"
	}
	if in.Manual {
		operation = "manual topup"
	}
	c.Effects.Sync(order.UserID, quota, operation)
	if in.Manual || (in.Provider != PaymentProviderWaffo && in.Provider != PaymentProviderWaffoPancake) || quota > 0 {
		provider := in.Provider
		if in.Manual {
			provider = "admin"
		}
		c.Effects.Receipt(order, quota, in.CallerIP, provider)
	}
	return alreadyDone, nil
}

// Transition changes a pending provider order without changing its completion timestamp.
func Transition(store Transactions, tradeNo, provider, status string) error {
	if tradeNo == "" {
		return ErrPaymentTradeNo
	}
	return store.Transaction(func(tx Tx) error {
		order, err := tx.FindLocked(tradeNo)
		if err != nil {
			return ErrNotFound
		}
		if provider != "" && order.PaymentProvider != provider {
			return ErrProviderMismatch
		}
		if order.Status != StatusPending {
			return ErrNotPending
		}
		order.Status = status
		return tx.Save(&order)
	})
}
