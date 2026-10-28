package dto

type EmailBindRequest struct {
	Email     string `json:"email"`
	FlowToken string `json:"flow_token"`
	NewCode   string `json:"new_code"`
	OldCode   string `json:"old_code"`
}
