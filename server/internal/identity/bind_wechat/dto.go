package bind_wechat

import "github.com/QuantumNous/new-api/internal/identity/domain"

type Request struct {
	Code string `json:"code"`
}
type Input struct {
	Identity domain.AuthIdentity
	Code     string
}
type Result struct {
	Issue string
	Cause error
}
