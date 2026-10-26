package model

import "github.com/QuantumNous/new-api/internal/adapter/gorm/catalog/record"

// BRIDGE: Vendor keeps migrations and pricing readers on the catalog record; remove after their owner migration.
type Vendor = record.Vendor
