package aggregate_usage_data

import (
	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/internal/usage/transport"
)

type Handler struct {
	usecase Usecase
}

func NewHandler(usecase Usecase) Handler {
	return Handler{usecase: usecase}
}

func (h Handler) Handle(c *gin.Context) {
	var request Request
	if !transport.ReadAggregateRequest(c, &request) {
		return
	}
	result, err := h.usecase.Execute(domain.UsageAggregateQuery{
		AggregateScope: domain.AggregateScope{
			UserIDs: request.UserIDs, StartTimestamp: request.StartTimestamp, EndTimestamp: request.EndTimestamp,
		},
		GroupBy: request.GroupBy, Granularity: request.Granularity,
	})
	var wire []Row
	if result != nil {
		wire = make([]Row, len(result))
		for i, row := range result {
			wire[i] = Row(row)
		}
	}
	transport.RespondAggregate(c, wire, err)
}
