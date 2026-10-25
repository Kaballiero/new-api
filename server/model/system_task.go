package model

import "github.com/QuantumNous/new-api/internal/adapter/gorm/system/record"

// BRIDGE: model migrations and legacy record consumers; remove when they use the system record directly.
type SystemTask = record.SystemTask

// BRIDGE: model migrations and legacy record consumers; remove when they use the system record directly.
type SystemTaskLock = record.SystemTaskLock

const (
	// BRIDGE: legacy record consumers; remove when they use the system record directly.
	SystemTaskStatusPending = record.SystemTaskStatusPending
	// BRIDGE: legacy record consumers; remove when they use the system record directly.
	SystemTaskStatusRunning = record.SystemTaskStatusRunning
	// BRIDGE: legacy record consumers; remove when they use the system record directly.
	SystemTaskStatusSucceeded = record.SystemTaskStatusSucceeded
	// BRIDGE: legacy record consumers; remove when they use the system record directly.
	SystemTaskStatusFailed = record.SystemTaskStatusFailed
	// BRIDGE: legacy record consumers; remove when they use the system record directly.
	SystemTaskTypeChannelTest = record.SystemTaskTypeChannelTest
	// BRIDGE: legacy record consumers; remove when they use the system record directly.
	SystemTaskTypeModelUpdate = record.SystemTaskTypeModelUpdate
	// BRIDGE: legacy record consumers; remove when they use the system record directly.
	SystemTaskTypePricingCatalog = record.SystemTaskTypePricingCatalog
)
