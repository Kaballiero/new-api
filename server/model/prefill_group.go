package model

import "github.com/QuantumNous/new-api/internal/adapter/gorm/catalog/record"

// BRIDGE: JSONValue keeps existing migrations and metadata consumers on the catalog codec; remove after they import its record owner.
type JSONValue = record.JSONValue

// BRIDGE: PrefillGroup keeps existing migrations on the same table; remove when migrations use the catalog record directly.
type PrefillGroup = record.PrefillGroup
