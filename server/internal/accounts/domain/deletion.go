package domain

// DeletionSession binds an account deletion to the current user and session versions.
type DeletionSession struct {
	UserID          int
	SessionID       string
	UserAuthVersion int64
	SessionVersion  int64
}
