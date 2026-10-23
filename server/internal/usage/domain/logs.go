package domain

import (
	"errors"
	"unicode/utf8"
)

// Persisted log kinds have fixed values; adding a kind must not renumber existing rows.
const (
	LogTypeUnknown = 0
	LogTypeTopup   = 1
	LogTypeConsume = 2
	LogTypeManage  = 3
	LogTypeSystem  = 4
	LogTypeError   = 5
	LogTypeRefund  = 6
	LogTypeLogin   = 7
)

var ErrTokenNotFound = errors.New("token not found")

var ErrLogQuery = errors.New("failed to query logs")

var ErrLogStatsQuery = errors.New("failed to query statistics")

const (
	MaxLogSearchCount = 10000
	MaxLogQueryUsers  = 16384
	MaxLogModelBytes  = 16 << 10
	MaxLogUserID      = 1<<31 - 1
)

type LogQuery struct {
	Page                                int
	CountLimit                          int
	Kind, UserID, TokenID               int
	UserIDs                             []int
	StartTimestamp, EndTimestamp        int64
	ModelName, Username, TokenName      string
	Channel                             int
	Group, RequestID, UpstreamRequestID string
	Start, Count                        int
	LiteralModel                        bool
}

type Stat struct {
	Quota, Rpm, Tpm int
}

var ErrInvalidLogQuery = errors.New("invalid log query")

func (q *LogQuery) ValidateStrict() error {
	maxInt := int(^uint(0) >> 1)
	if len(q.UserIDs) > MaxLogQueryUsers || q.Page <= 0 || q.Count < 1 || q.Count > 100 ||
		q.Page-1 > (maxInt-q.Count)/q.Count || q.Kind < LogTypeUnknown || q.Kind > LogTypeLogin ||
		q.StartTimestamp < 0 || q.EndTimestamp < 0 ||
		(q.StartTimestamp != 0 && q.EndTimestamp != 0 && q.StartTimestamp > q.EndTimestamp) ||
		len(q.ModelName) > MaxLogModelBytes || !utf8.ValidString(q.ModelName) {
		return ErrInvalidLogQuery
	}
	q.Start = (q.Page - 1) * q.Count
	return nil
}
