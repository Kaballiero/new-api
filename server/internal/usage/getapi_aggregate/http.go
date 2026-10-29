package getapi_aggregate

import (
	"github.com/QuantumNous/new-api/internal/usage/transport"
	"github.com/gin-gonic/gin"
)

type Handler struct{ usecase Usecase }

func NewHandler(usecase Usecase) Handler { return Handler{usecase: usecase} }

func (h Handler) Handle(c *gin.Context) {
	var request GetAPIUsageAggregateRequest
	if !transport.ReadGetAPIUsageRequest(c, &request) {
		return
	}
	result, err := h.usecase.Execute(request)
	transport.RespondGetAPIUsage(c, result, err)
}
