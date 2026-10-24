package model

import "github.com/QuantumNous/new-api/internal/adapter/gorm/subscriptions/record"

type SubscriptionPlan = record.SubscriptionPlan
type SubscriptionOrder = record.SubscriptionOrder
type UserSubscription = record.UserSubscription
type SubscriptionPreConsumeRecord = record.SubscriptionPreConsumeRecord

const (
	SubscriptionDurationYear   = record.SubscriptionDurationYear
	SubscriptionDurationMonth  = record.SubscriptionDurationMonth
	SubscriptionDurationDay    = record.SubscriptionDurationDay
	SubscriptionDurationHour   = record.SubscriptionDurationHour
	SubscriptionDurationCustom = record.SubscriptionDurationCustom
	SubscriptionResetNever     = record.SubscriptionResetNever
	SubscriptionResetDaily     = record.SubscriptionResetDaily
	SubscriptionResetWeekly    = record.SubscriptionResetWeekly
	SubscriptionResetMonthly   = record.SubscriptionResetMonthly
	SubscriptionResetCustom    = record.SubscriptionResetCustom
)
