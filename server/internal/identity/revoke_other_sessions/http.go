package revoke_other_sessions

import (
	identityhttp "github.com/QuantumNous/new-api/internal/adapter/identity/http"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ revoker Revoker }

func NewHTTP(revoker Revoker) *HTTP { return &HTTP{revoker: revoker} }
func (h *HTTP) Handle(c *gin.Context) {
	identity, ok := identityhttp.RequireBrowserSession(c)
	if !ok {
		return
	}
	result, err := NewUsecase(h.revoker).Execute(Input{UserID: identity.UserID, CurrentSID: identity.SessionID})
	if err != nil {
		identityhttp.WriteAuthSessionError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"revoked_count": result.Count}})
}
