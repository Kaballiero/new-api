package get_verification_requirements

import identitydto "github.com/QuantumNous/new-api/internal/identity/dto"

type VerificationOAuthProvider = identitydto.VerificationOAuthProvider

type VerificationRequirements struct {
	Scope                     string                                 `json:"scope"`
	Methods                   []identitydto.VerificationMethodOption `json:"methods"`
	OAuthProviders            []VerificationOAuthProvider            `json:"oauth_providers"`
	PasswordEncryptionEnabled bool                                   `json:"password_encryption_enabled"`
}
