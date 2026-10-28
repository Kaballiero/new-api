package dto

type SecurityProof struct {
	ProofToken string `json:"proof_token"`
	ExpiresAt  int64  `json:"expires_at"`
	Method     string `json:"method"`
	Scope      string `json:"scope"`
}

type VerificationMethodOption struct {
	Method    string `json:"method"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

type VerificationOAuthProvider struct {
	Slug   string `json:"slug"`
	Name   string `json:"name"`
	UserID string `json:"-"`
}
