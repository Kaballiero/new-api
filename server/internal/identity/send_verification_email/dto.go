package send_verification_email

// Input is the address from the query parameter and the request language of
// the e-mail.
type Input struct {
	Email string
	Lang  string
}
