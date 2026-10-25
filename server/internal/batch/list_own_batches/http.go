package list_own_batches

import (
	"github.com/QuantumNous/new-api/i18n"
	"strconv"

	"github.com/QuantumNous/new-api/internal/batch/domain"
	"github.com/QuantumNous/new-api/internal/batch/dto"
	"github.com/QuantumNous/new-api/internal/batch/transport"
	"github.com/gin-gonic/gin"
)

type Handler struct{ usecase Usecase }

func NewHandler(u Usecase) Handler { return Handler{u} }
func (h Handler) Handle(c *gin.Context) {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if err != nil {
		transport.Error(c, &domain.RuleError{Kind: domain.InvalidLimit})
		return
	}
	result, err := h.usecase.Execute(c, Input{UserID: c.GetInt("id"), Limit: limit, After: c.Query("after")})
	if err != nil {
		transport.Error(c, err)
		return
	}
	var data []dto.LocalizedJob
	if result.Jobs != nil {
		data = make([]dto.LocalizedJob, len(result.Jobs))
	}
	for i := range result.Jobs {
		data[i] = dto.Job(result.Jobs[i]).WithLanguage(i18n.GetLangFromContext(c))
	}
	c.JSON(200, gin.H{"object": "list", "data": data, "has_more": result.HasMore})
}
