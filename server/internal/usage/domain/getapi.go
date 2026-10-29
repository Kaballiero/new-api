package domain

import "errors"

var ErrGetAPIUsageUnsupported = errors.New("GETAPI_USAGE_UNSUPPORTED")

var ErrGetAPIUsageInvalidQuery = errors.New("GETAPI_USAGE_INVALID_QUERY")

func ValidateGetAPIUsageWindow(start, end *int64) error {
	if start == nil || end == nil || *start < 0 || *end < *start || *end-*start > 93*86400 {
		return ErrGetAPIUsageInvalidQuery
	}
	return nil
}
