package revoke_other_sessions

import (
	"net/http"

	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	revoker    Revoker
	identity   func(*gin.Context) (domain.AuthIdentity, bool)
	writeError func(*gin.Context, error)
}

func NewHTTP(revoker Revoker, identity func(*gin.Context) (domain.AuthIdentity, bool), writeError func(*gin.Context, error)) *HTTP {
	return &HTTP{revoker: revoker, identity: identity, writeError: writeError}
}
func (h *HTTP) Handle(c *gin.Context) {
	identity, ok := transport.RequireBrowserSession(c, h.identity)
	if !ok {
		return
	}
	result, err := NewUsecase(h.revoker).Execute(Input{UserID: identity.UserID, CurrentSID: identity.SessionID})
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"revoked_count": result.Count}})
}
