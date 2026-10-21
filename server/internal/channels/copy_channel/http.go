package copy_channel

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/internal/channels/transport"
	"github.com/gin-gonic/gin"
)

// defaultSuffix is appended to the copy name when the client sends no suffix.
// It is a stored data default kept from the original implementation, not a
// message.
const defaultSuffix = "_复制"

type HTTP struct{ usecase func(*gin.Context) Usecase }

func NewHTTP(usecase func(*gin.Context) Usecase) HTTP { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "invalid id"})
		return
	}
	suffix := c.DefaultQuery("suffix", defaultSuffix)
	resetBalance := true
	if rbStr := c.DefaultQuery("reset_balance", "true"); rbStr != "" {
		if v, err := strconv.ParseBool(rbStr); err == nil {
			resetBalance = v
		}
	}
	output, err := h.usecase(c).Execute(Input{ID: id, Suffix: suffix, ResetBalance: resetBalance})
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"id": output.ID}})
}
