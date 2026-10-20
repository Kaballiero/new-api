package dto

import "encoding/json"

type PasskeyFinishRequest struct {
	FlowToken  string          `json:"flow_token"`
	Credential json.RawMessage `json:"credential"`
}
type PasskeyVerifyBeginRequest struct {
	Scope   string          `json:"scope"`
	Context json.RawMessage `json:"context,omitempty"`
}
type PasskeyChallenge struct {
	Options   any
	FlowToken string
	ExpiresAt int64
}
