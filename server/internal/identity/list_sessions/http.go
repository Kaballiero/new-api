package list_sessions

import (
	identityhttp "github.com/QuantumNous/new-api/internal/adapter/identity/http"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ reader Reader }

func NewHTTP(reader Reader) *HTTP { return &HTTP{reader: reader} }
func (h *HTTP) Handle(c *gin.Context) {
	identity, ok := identityhttp.RequireBrowserSession(c)
	if !ok {
		return
	}
	sessions, err := NewUsecase(h.reader).Execute(Input{UserID: identity.UserID, CurrentSID: identity.SessionID})
	if err != nil {
		identityhttp.WriteAuthSessionError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": sessions})
}
