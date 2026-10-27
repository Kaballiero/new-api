package verification

import (
	"encoding/json"
	"github.com/QuantumNous/new-api/common"
	accountstore "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/core"

	"github.com/QuantumNous/new-api/oauth"
)

type ContextCodec struct {
	SigningKey func(string) []byte
}

func (ContextCodec) Object(raw json.RawMessage) bool    { return common.GetJsonType(raw) == "object" }
func (ContextCodec) Decode(raw []byte, value any) error { return common.Unmarshal(raw, value) }
func (ContextCodec) NormalizeEmail(email string) string { return accountstore.NormalizeEmail(email) }
func (ContextCodec) EmailValid(email string) bool {
	return common.Validate.Var(email, "required,email") == nil
}
func (ContextCodec) ProviderExists(name string) bool { return oauth.GetProvider(name) != nil }
func (op ContextCodec) Digest(scope string, context any) (string, error) {
	payload, err := common.Marshal(struct {
		Scope   string `json:"scope"`
		Context any    `json:"context"`
	}{Scope: scope, Context: context})
	if err != nil {
		return "", err
	}
	return common.GenerateHMACWithKey(op.SigningKey("verification-context"), string(payload)), nil
}
