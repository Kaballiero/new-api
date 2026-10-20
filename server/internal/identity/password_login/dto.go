package password_login

type LoginRequest struct {
	Username          string `json:"username"`
	Password          string `json:"password"`
	PasswordEncrypted string `json:"password_encrypted"`
	EncryptionKeyID   string `json:"encryption_key_id"`
}
type Account struct {
	ID          int
	Status      int
	AuthVersion int64
}
type Result struct {
	Account Account
	Issue   string
	Cause   error
}
