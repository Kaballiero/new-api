package admin_unlink_oauth_account

type Input struct{ UserID int }
type Result struct {
	Forbidden, InvalidProvider bool
	Cause                      error
}
