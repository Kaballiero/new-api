package finish_twofa_setup

type Verify2FARequest struct {
	Code      string `json:"code" binding:"required"`
	FlowToken string `json:"flow_token,omitempty"`
}
