package get_passkey_status

import (
	identityhttp "github.com/QuantumNous/new-api/internal/adapter/identity/http"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ reader Reader }

func NewHTTP(reader Reader) *HTTP { return &HTTP{reader: reader} }
func (h *HTTP) Handle(c *gin.Context) {
	user, err := identityhttp.GetAuthenticatedUser(c)
	if err != nil {
		identityhttp.WriteSecurityOperationError(c, err)
		return
	}
	result, err := NewUsecase(h.reader).Execute(user.Id)
	if err != nil {
		identityhttp.WriteSecurityOperationError(c, err)
		return
	}
	data := gin.H{"enabled": result.Enabled}
	if result.Enabled {
		data["last_used_at"] = result.LastUsedAt
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": data})
}
