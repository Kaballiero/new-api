package common

import (
	"encoding/json"
	"strings"
)

// PreserveJSONTypeError keeps the public decoder type name when a wire DTO moves packages.
// Only direct decoder errors for the explicitly owned package prefix are adapted.
func PreserveJSONTypeError(err error, ownedPrefix, originalPackage string) error {
	mismatch, ok := err.(*json.UnmarshalTypeError)
	if !ok || mismatch == nil || !strings.HasPrefix(mismatch.Type.PkgPath(), ownedPrefix) {
		return err
	}
	oldType := originalPackage + "." + mismatch.Type.Name()
	message := "json: cannot unmarshal " + mismatch.Value
	if mismatch.Struct != "" || mismatch.Field != "" {
		message += " into Go struct field " + mismatch.Struct + "." + mismatch.Field + " of type " + oldType
	} else {
		message += " into Go value of type " + oldType
	}
	return &legacyJSONTypeError{cause: err, message: message}
}

type legacyJSONTypeError struct {
	cause   error
	message string
}

func (e *legacyJSONTypeError) Error() string { return e.message }
func (e *legacyJSONTypeError) Unwrap() error { return e.cause }
