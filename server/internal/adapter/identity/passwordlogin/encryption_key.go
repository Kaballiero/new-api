package passwordlogin

import "github.com/QuantumNous/new-api/common"

type EncryptionKey struct{}

func (EncryptionKey) Enabled() bool               { return common.PasswordLoginEncryptionEnabled }
func (EncryptionKey) PublicKey() (string, string) { return common.PasswordEncryptionPublicKey() }
