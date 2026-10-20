package domain

import (
	"slices"
	"strings"
)

type EmailPolicy struct {
	Normalize           func(string) string
	Valid               func(string) bool
	DomainRestriction   func() bool
	Whitelist           func() []string
	AliasRestriction    func() bool
	Invalid, Restricted func() error
}

func ValidateEmail(email string, p EmailPolicy) (string, error) {
	email = p.Normalize(email)
	if !p.Valid(email) {
		return "", p.Invalid()
	}
	local, host, found := strings.Cut(email, "@")
	if !found || strings.Contains(host, "@") {
		return "", p.Invalid()
	}
	if p.DomainRestriction() && !slices.Contains(p.Whitelist(), host) {
		return "", p.Restricted()
	}
	if p.AliasRestriction() && strings.ContainsAny(local, "+.") {
		return "", p.Restricted()
	}
	return email, nil
}
