package get_options

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type Handler struct{ usecase Usecase }

func NewHandler(usecase Usecase) Handler { return Handler{usecase} }
func (h Handler) Handle(c *gin.Context) {
	options, err := h.usecase.Execute()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	data := make([]Option, 0, len(options))
	for _, option := range options {
		data = append(data, Option{Key: option.Key, Value: option.Value})
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": data})
}
