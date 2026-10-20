package domain

// PasswordCredential is the account state required by password authentication.
// Loading the account and owning the login operation belong to identity.
type PasswordCredential struct {
	Hash    string
	Enabled bool
}

func (credential PasswordCredential) Accepts(password string, verify func(string, string) bool) bool {
	return credential.Hash != "" && verify(password, credential.Hash) && credential.Enabled
}
