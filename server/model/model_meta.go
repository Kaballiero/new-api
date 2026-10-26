package model

import "github.com/QuantumNous/new-api/internal/adapter/gorm/catalog/record"

// BRIDGE: Model keeps migrations and pricing readers on the catalog record; remove after their owner migration.
type Model = record.Model

// BRIDGE: BoundChannel keeps migrations and pricing readers on the catalog record; remove after their owner migration.
type BoundChannel = record.BoundChannel

// BRIDGE: ModelSquareState keeps migrations and pricing readers on the catalog record; remove after their owner migration.
type ModelSquareState = record.ModelSquareState

// BRIDGE: NameRuleExact preserves catalog facts for existing record consumers; remove when callers import the record owner.
const NameRuleExact = record.NameRuleExact

// BRIDGE: NameRulePrefix preserves catalog facts for existing record consumers; remove when callers import the record owner.
const NameRulePrefix = record.NameRulePrefix

// BRIDGE: NameRuleContains preserves catalog facts for existing record consumers; remove when callers import the record owner.
const NameRuleContains = record.NameRuleContains

// BRIDGE: NameRuleSuffix preserves catalog facts for existing record consumers; remove when callers import the record owner.
const NameRuleSuffix = record.NameRuleSuffix

// BRIDGE: ModelSquareVisible preserves catalog facts for existing record consumers; remove when callers import the record owner.
const ModelSquareVisible = record.ModelSquareVisible

// BRIDGE: ModelSquareUnavailable preserves catalog facts for existing record consumers; remove when callers import the record owner.
const ModelSquareUnavailable = record.ModelSquareUnavailable

// BRIDGE: ModelSquareHidden preserves catalog facts for existing record consumers; remove when callers import the record owner.
const ModelSquareHidden = record.ModelSquareHidden

// BRIDGE: ModelSquarePartial preserves catalog facts for existing record consumers; remove when callers import the record owner.
const ModelSquarePartial = record.ModelSquarePartial
