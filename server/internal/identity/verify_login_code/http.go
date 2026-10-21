package verify_login_code

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/gin-gonic/gin"
)

type HTTP struct {
	verify     func(string, string, string, string) (*dto.AuthBundle, error)
	complete   func(*gin.Context, *dto.AuthBundle, string)
	writeError func(*gin.Context, error)
}

func NewHTTP(verify func(string, string, string, string) (*dto.AuthBundle, error), complete func(*gin.Context, *dto.AuthBundle, string), writeError func(*gin.Context, error)) *HTTP {
	return &HTTP{verify: verify, complete: complete, writeError: writeError}
}
func (h *HTTP) Handle(c *gin.Context) {
	var request Request
	if common.DecodeJson(c.Request.Body, &request) != nil || request.FlowToken == "" || request.Code == "" {
		common.ApiErrorI18n(c, i18n.MsgIdentityParamsError)
		return
	}
	if request.Method == "" {
		request.Method = "2fa"
	}
	if request.Method != "2fa" {
		h.writeError(c, domain.ErrProofMethod)
		return
	}
	bundle, err := h.verify(request.FlowToken, request.Code, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.complete(c, bundle, "2fa")
}
