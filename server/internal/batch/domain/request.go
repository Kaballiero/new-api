package domain

import "encoding/json"

const MaxBatchRequestBytes = 16 << 20
const MaxBatchRequests = 1000

type CreateRequest struct {
	Model    string          `json:"model"`
	Group    string          `json:"group,omitempty"`
	Endpoint string          `json:"endpoint"`
	Provider json.RawMessage `json:"provider,omitempty"`
	Requests []RequestItem   `json:"requests"`
}
type PreparedRequest struct {
	Request                        CreateRequest
	PublicID, KeyHash, RequestHash string
	Existing                       *Job
}
