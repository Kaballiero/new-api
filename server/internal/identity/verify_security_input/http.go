package verify_security_input

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct {
	identity   func(*gin.Context) (domain.AuthIdentity, bool)
	verify     func(domain.AuthIdentity, VerificationInput) (*dto.SecurityProof, error)
	writeError func(*gin.Context, error)
	audit      func(*gin.Context, int, string, map[string]any)
}

func NewHTTP(identity func(*gin.Context) (domain.AuthIdentity, bool), verify func(domain.AuthIdentity, VerificationInput) (*dto.SecurityProof, error), writeError func(*gin.Context, error), audit func(*gin.Context, int, string, map[string]any)) *HTTP {
	return &HTTP{identity: identity, verify: verify, writeError: writeError, audit: audit}
}
func (h *HTTP) Handle(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": i18n.T(c, i18n.MsgIdentitySecurityVerificationUnsupported)})
		return
	}
	var request VerificationInput
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgIdentityParamsError)
		return
	}
	proof, err := h.verify(identity, request)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.audit(c, identity.UserID, "user.security_verify", map[string]any{"method": proof.Method, "scope": proof.Scope})
	common.ApiSuccess(c, proof)
}
