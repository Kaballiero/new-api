// Package taskbillingruntime assembles task accounting and recipient-text capabilities.
package taskbillingruntime

import (
	"sync"

	effects "github.com/QuantumNous/new-api/internal/adapter/taskruntime"
	"github.com/QuantumNous/new-api/internal/app/accountstorage"
	"github.com/QuantumNous/new-api/internal/app/apikeystorage"
	billing "github.com/QuantumNous/new-api/internal/app/billing"
)

type runtime struct {
	delta      effects.TaskDelta
	prices     effects.TaskPrices
	midjourney effects.MidjourneyBilling
	text       effects.TaskText
}

var shared = sync.OnceValue(func() runtime {
	store := billing.NewStore()
	tokens, quotas := apikeystorage.Persistence(), apikeystorage.Quota()
	return runtime{delta: effects.TaskDelta{Store: store, Tokens: tokens, Quotas: quotas}, prices: effects.TaskPrices{Accounts: accountstorage.SQL()},
		midjourney: effects.MidjourneyBilling{Store: store, Tokens: tokens, Quotas: quotas}, text: effects.TaskText{Language: accountstorage.Cache().GetUserLanguage}}
})

func Delta() effects.TaskDelta              { return shared().delta }
func Prices() effects.TaskPrices            { return shared().prices }
func Midjourney() effects.MidjourneyBilling { return shared().midjourney }
func Text() effects.TaskText                { return shared().text }
