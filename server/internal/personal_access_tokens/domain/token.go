package domain

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

type Account struct {
	Token     string
	Role      int
	CreatedAt *int64
}
type Usage struct {
	CreatedAt int64
	Ip        string
}
type AuditRoles struct{ Root, Common, Admin int }

// Fingerprint normalizes PostgreSQL CHAR padding before identifying a generation.
func Fingerprint(token string) string {
	token = strings.TrimRight(token, " ")
	if token == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", digest)
}
