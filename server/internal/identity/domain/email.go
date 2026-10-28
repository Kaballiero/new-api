package domain

import (
	"slices"
	"strings"
)

type EmailPolicy struct {
	Normalize         func(string) string
	Valid             func(string) bool
	DomainRestriction func() bool
	Whitelist         func() []string
	AliasRestriction  func() bool
}

func ValidateEmail(email string, p EmailPolicy) (string, error) {
	email = p.Normalize(email)
	if !p.Valid(email) {
		return "", ErrEmailInvalid
	}
	local, host, found := strings.Cut(email, "@")
	if !found || strings.Contains(host, "@") {
		return "", ErrEmailInvalid
	}
	if p.DomainRestriction() && !slices.Contains(p.Whitelist(), host) {
		return "", ErrEmailRestricted
	}
	if p.AliasRestriction() && strings.ContainsAny(local, "+.") {
		return "", ErrEmailRestricted
	}
	return email, nil
}
