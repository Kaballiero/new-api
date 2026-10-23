package model

import "github.com/QuantumNous/new-api/internal/adapter/gorm/audit/record"

type AuditLog = record.AuditLog

type AuditOther = record.AuditOther

type AuditOperation = record.AuditOperation

type AuditAdminInfo = record.AuditAdminInfo

type AuditRequestInfo = record.AuditRequestInfo

type AuditFields = record.AuditFields

const (
	AuditCategoryLogin       = record.AuditCategoryLogin
	AuditCategorySecurity    = record.AuditCategorySecurity
	AuditCategoryOperation   = record.AuditCategoryOperation
	AuditCategoryAccessToken = record.AuditCategoryAccessToken
)
