package start_oauth_flow

import "encoding/json"

type Request struct {
	Provider string          `json:"provider"`
	Intent   string          `json:"intent"`
	Aff      string          `json:"aff,omitempty"`
	Scope    string          `json:"scope,omitempty"`
	Context  json.RawMessage `json:"context,omitempty"`
}
type Result struct {
	Issue string
	Cause error
}
