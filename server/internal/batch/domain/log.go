package domain

// LogAccount is the account identity already read under the settlement locks.
type LogAccount struct {
	UserID, TokenID     int
	Username, TokenName string
}
