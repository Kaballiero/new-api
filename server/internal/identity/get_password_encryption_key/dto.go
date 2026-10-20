package get_password_encryption_key

type Output struct {
	Enabled          bool
	KeyID, PublicKey string
	Unavailable      bool
}
