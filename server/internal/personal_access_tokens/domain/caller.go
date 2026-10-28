package domain

// Caller identifies a PAT owner without exposing the credential or account secrets.
type Caller struct {
	Id       int
	Username string
	Role     int
	TokenRef string
}
