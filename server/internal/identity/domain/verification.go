package domain

import "encoding/json"

type VerificationOperation struct {
	Scope   string          `json:"scope"`
	Context json.RawMessage `json:"context,omitempty"`
}
type VerificationBinding struct {
	Scope       string `json:"scope"`
	ContextHash string `json:"context_hash"`
}
