package model

import "github.com/QuantumNous/new-api/internal/adapter/gorm/system/record"

// BRIDGE: model migrations and legacy record consumers; remove when they use the system record directly.
type Setup = record.Setup
