package aggregate_logs

import (
	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/internal/usage/dto"
	"github.com/QuantumNous/new-api/internal/usage/transport"
)

type Handler struct {
	usecase Usecase
}

func NewHandler(usecase Usecase) Handler {
	return Handler{usecase: usecase}
}

func (h Handler) Handle(c *gin.Context) {
	var request dto.AggregateScopeRequest
	if !transport.ReadAggregateRequest(c, &request) {
		return
	}
	result, err := h.usecase.Execute(domain.AggregateScope{
		UserIDs: request.UserIDs, StartTimestamp: request.StartTimestamp, EndTimestamp: request.EndTimestamp,
	})
	wire := Response{Quota: result.Quota, RequestCount: result.RequestCount}
	transport.RespondAggregate(c, wire, err)
}
