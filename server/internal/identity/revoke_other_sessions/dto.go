package revoke_other_sessions

type Input struct {
	UserID     int
	CurrentSID string
}
type Output struct{ Count int64 }
