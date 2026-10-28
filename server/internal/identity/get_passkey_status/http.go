package get_passkey_status

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	reader     Reader
	userID     func(*gin.Context) (int, error)
	writeError func(*gin.Context, error)
}

// NewHTTP builds the handler; userID returns the enabled authenticated user.
func NewHTTP(reader Reader, userID func(*gin.Context) (int, error), writeError func(*gin.Context, error)) *HTTP {
	return &HTTP{reader: reader, userID: userID, writeError: writeError}
}
func (h *HTTP) Handle(c *gin.Context) {
	userID, err := h.userID(c)
	if err != nil {
		h.writeError(c, err)
		return
	}
	result, err := NewUsecase(h.reader).Execute(userID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	data := gin.H{"enabled": result.Enabled}
	if result.Enabled {
		data["last_used_at"] = result.LastUsedAt
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": data})
}
