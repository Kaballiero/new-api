package get_twofa_status

import (
	identityhttp "github.com/QuantumNous/new-api/internal/adapter/identity/http"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ reader Reader }

func NewHTTP(reader Reader) *HTTP { return &HTTP{reader: reader} }
func (h *HTTP) Handle(c *gin.Context) {
	status, err := NewUsecase(h.reader).Execute(c.GetInt("id"))
	if err != nil {
		identityhttp.WriteSecurityOperationError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": status})
}
