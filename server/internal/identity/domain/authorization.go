package domain

type AuthSessionIdentity = AuthIdentity
type FlowAuthorization struct {
	AuthSessionIdentity
	ProofID     int64  `json:"proof_id"`
	Scope       string `json:"scope"`
	ContextHash string `json:"context_hash"`
	Method      string `json:"method"`
}
