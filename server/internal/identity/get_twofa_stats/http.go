package get_twofa_stats

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	reader     Reader
	writeError func(*gin.Context, error)
}

func NewHTTP(reader Reader, writeError func(*gin.Context, error)) *HTTP {
	return &HTTP{reader: reader, writeError: writeError}
}
func (h *HTTP) Handle(c *gin.Context) {
	status, err := NewUsecase(h.reader).Execute()
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": status})
}
