package finish_login_passkey

import "encoding/json"

type Request struct {
	FlowToken        string          `json:"flow_token"`
	PasskeyFlowToken string          `json:"passkey_flow_token"`
	Credential       json.RawMessage `json:"credential"`
}
