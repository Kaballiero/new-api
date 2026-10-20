package domain

type PasskeyFlowSecurity struct {
	AuthSessionIdentity
	Scope          string             `json:"scope"`
	ContextHash    string             `json:"context_hash"`
	Authorization  *FlowAuthorization `json:"authorization,omitempty"`
	LoginFlowID    int64              `json:"login_flow_id,omitempty"`
	LoginExpiresAt int64              `json:"login_expires_at,omitempty"`
}
