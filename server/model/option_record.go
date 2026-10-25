package model

import "github.com/QuantumNous/new-api/internal/adapter/gorm/settings/record"

// BRIDGE: model migrations and retained pricing/catalog consumers still use model.Option.
// Remove when those consumers use the settings record directly.
type Option = record.Option
