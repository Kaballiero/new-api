package revoke_session

type Input struct {
	UserID int
	SID    string
}
type Output struct {
	SID     string
	Revoked bool
	Invalid bool
}
