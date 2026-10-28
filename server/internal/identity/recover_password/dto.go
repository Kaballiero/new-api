package recover_password

type PasswordResetRequest struct {
	Email string `json:"email"`
	Token string `json:"token"`
}
