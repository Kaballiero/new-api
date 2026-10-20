package recover_password

type PasswordResetRequest struct {
	Email string `json:"email"`
	Token string `json:"token"`
}
type PasswordRecoveryResult struct {
	InvalidInput, InvalidLink bool
	Password                  string
}
