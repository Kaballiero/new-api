package domain

type AccountState struct {
	Status      int
	AuthVersion int64
}
type SessionState struct {
	UserID          int
	UserAuthVersion int64
	Version         int64
	Status          string
	RevokedAt       int64
	ExpiresAt       int64
}

// SessionPolicy keeps the current configuration read at the original decision points.
type SessionPolicy struct {
	EnabledStatus         int
	ActiveStatus          string
	ActiveLimit           func() int64
	IssuanceWindowSeconds func() int64
	IssuanceLimit         func() int64
}
