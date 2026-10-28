package domain

import (
	"errors"
	"slices"
	"strconv"
	"strings"
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
	Kinds                               []int
	StartTimestamp, EndTimestamp        int64
	ModelName, Username, TokenName      string
	Channel                             int
	Group, RequestID, UpstreamRequestID string
	Start, Count                        int
	LiteralModel                        bool
}

type Stat struct {
	Quota, RefundQuota, Rpm, Tpm int
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
	if q.Kinds != nil {
		var valid bool
		q.Kinds, valid = NormalizeLogTypes(q.Kinds)
		if !valid {
			return ErrInvalidLogQuery
		}
	}
	q.Start = (q.Page - 1) * q.Count
	return nil
}

// LogTypes preserves the legacy type filter when the multi-type filter is absent.
func (q LogQuery) LogTypes() []int {
	if q.Kinds != nil {
		return q.Kinds
	}
	if q.Kind == LogTypeUnknown {
		return nil
	}
	return []int{q.Kind}
}

// NormalizeLogTypes validates known kinds and preserves first occurrence order.
func NormalizeLogTypes(kinds []int) ([]int, bool) {
	if len(kinds) == 0 {
		return nil, false
	}
	normalized := make([]int, 0, len(kinds))
	for _, kind := range kinds {
		if kind < LogTypeTopup || kind > LogTypeLogin {
			return nil, false
		}
		if !slices.Contains(normalized, kind) {
			normalized = append(normalized, kind)
		}
	}
	return normalized, true
}

// ParseLogTypesQuery accepts one comma-separated parameter, rejecting padding,
// signs, spaces and repeated parameters exactly as the upstream API does.
func ParseLogTypesQuery(values []string) ([]int, bool) {
	if len(values) != 1 {
		return nil, false
	}
	var kinds []int
	for part := range strings.SplitSeq(values[0], ",") {
		kind, err := strconv.Atoi(part)
		if err != nil || len(part) != 1 {
			return nil, false
		}
		kinds = append(kinds, kind)
	}
	return NormalizeLogTypes(kinds)
}
