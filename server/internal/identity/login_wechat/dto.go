package login_wechat

// Input is the WeChat code and the request language of the persisted
// registration texts.
type Input struct {
	Code string
	Lang string
}
