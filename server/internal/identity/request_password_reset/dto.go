package request_password_reset

// Input is the address from the existing query parameter and the request
// language of the e-mail.
type Input struct {
	Email string
	Lang  string
}
