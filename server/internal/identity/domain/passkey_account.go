package domain

// PasskeyAccount identifies the authenticated account at the transport boundary.
type PasskeyAccount struct {
	ID          int
	AuthVersion int64
}
