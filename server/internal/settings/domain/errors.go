package domain

import "errors"

var ErrComplianceRequired = errors.New("payment compliance required")
var ErrComplianceField = errors.New("compliance field requires dedicated endpoint")
var ErrRetiredTheme = errors.New("retired frontend theme")

type InvalidOption struct {
	Kind  string
	Model string
	Cause error
}

func (e *InvalidOption) Error() string {
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return e.Kind
}
func (e *InvalidOption) Unwrap() error { return e.Cause }
