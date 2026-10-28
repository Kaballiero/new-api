package verify_login_code

type Request struct {
	FlowToken string `json:"flow_token"`
	Method    string `json:"method"`
	Code      string `json:"code"`
}
