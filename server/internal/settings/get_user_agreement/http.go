package get_user_agreement

import (
	"github.com/QuantumNous/new-api/internal/settings/transport"
	"github.com/gin-gonic/gin"
)

type Handler struct{ read func() string }

func NewHandler(read func() string) Handler { return Handler{read} }
func (h Handler) Handle(c *gin.Context)     { transport.ServeRevalidatedJSON(c, h.read()) }
