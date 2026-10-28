package get_status

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	usecase Usecase
	lock    func() func()
}

func NewHandler(usecase Usecase, lock func() func()) Handler { return Handler{usecase, lock} }
func (h Handler) Handle(c *gin.Context) {
	unlock := h.lock()
	defer unlock()
	data, _ := h.usecase.Execute()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": data})
}
