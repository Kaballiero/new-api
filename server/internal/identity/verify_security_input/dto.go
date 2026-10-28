package verify_security_input

import "encoding/json"

type VerificationInput struct {
	Method            string          `json:"method"`
	Scope             string          `json:"scope"`
	Context           json.RawMessage `json:"context,omitempty"`
	Code              string          `json:"code,omitempty"`
	Password          string          `json:"password,omitempty"`
	PasswordEncrypted string          `json:"password_encrypted,omitempty"`
	EncryptionKeyID   string          `json:"encryption_key_id,omitempty"`
}
