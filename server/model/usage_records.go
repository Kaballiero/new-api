package model

import "github.com/QuantumNous/new-api/internal/adapter/gorm/usage/record"

type Log = record.Log

type LogOther = record.LogOther

type QuotaData = record.QuotaData

type QuotaDataLogParams = record.QuotaDataLogParams

const (
	LogTypeUnknown = record.LogTypeUnknown
	LogTypeTopup   = record.LogTypeTopup
	LogTypeConsume = record.LogTypeConsume
	LogTypeManage  = record.LogTypeManage
	LogTypeSystem  = record.LogTypeSystem
	LogTypeError   = record.LogTypeError
	LogTypeRefund  = record.LogTypeRefund
	LogTypeLogin   = record.LogTypeLogin
)
