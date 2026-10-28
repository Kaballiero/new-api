package fetch_task_plugin_icon

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/taskplugins/domain"
	"github.com/QuantumNous/new-api/internal/taskplugins/transport"

	"github.com/gin-gonic/gin"
)

type Handler struct{ usecase Usecase }

func NewHandler(u Usecase) Handler { return Handler{usecase: u} }
func (h Handler) ServeHTTP(c *gin.Context) {
	v, e := h.usecase.Execute(transport.Context(c), Input{Key: c.Param("key"), Version: c.Query("version")})
	if errors.Is(e, domain.ErrNotFound) {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	if e != nil {
		common.ApiError(c, e)
		return
	}
	c.Header("Cache-Control", "private, max-age=3600")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	c.Data(http.StatusOK, v.MediaType, v.Data)
}
