package login_wechat

type Input struct{ Code string }
type Result struct {
	Issue string
	Cause error
}
