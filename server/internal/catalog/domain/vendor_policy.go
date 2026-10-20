package domain

import (
	"errors"
	"strings"
	"unicode/utf8"
)

func NormalizeVendor(v *Vendor) error {
	v.Name = strings.TrimSpace(v.Name)
	v.Icon = strings.TrimSpace(v.Icon)
	if v.Name == "" {
		return errors.New("vendor name is required")
	}
	if utf8.RuneCountInString(v.Name) > 128 || utf8.RuneCountInString(v.Icon) > 128 {
		return errors.New("vendor name and icon must not exceed 128 characters")
	}
	return nil
}
