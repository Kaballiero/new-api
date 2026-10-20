package get_verification_requirements

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct {
	identity   func(*gin.Context) (domain.AuthIdentity, bool)
	resolve    func(domain.AuthIdentity, string) (*VerificationRequirements, error)
	writeError func(*gin.Context, error)
}

func NewHTTP(identity func(*gin.Context) (domain.AuthIdentity, bool), resolve func(domain.AuthIdentity, string) (*VerificationRequirements, error), writeError func(*gin.Context, error)) *HTTP {
	return &HTTP{identity: identity, resolve: resolve, writeError: writeError}
}
func (h *HTTP) Handle(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "当前认证方式不支持安全验证"})
		return
	}
	result, err := h.resolve(identity, c.Query("scope"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}
