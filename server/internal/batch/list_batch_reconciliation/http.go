package list_batch_reconciliation

import (
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/internal/batch/domain"
	"github.com/QuantumNous/new-api/internal/batch/transport"

	"github.com/gin-gonic/gin"
)

type Handler struct{ usecase Usecase }

func NewHandler(u Usecase) Handler { return Handler{u} }
func (h Handler) Handle(c *gin.Context) {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if err != nil || limit < 1 || limit > 100 {
		transport.Error(c, &domain.RuleError{Kind: domain.ReconciliationInvalidLimit})
		return
	}
	entries, more, err := h.usecase.Execute(Input{c.Query("before"), limit})
	if errors.Is(err, domain.ErrNotFound) {
		transport.Error(c, &domain.RuleError{Kind: domain.ReconciliationInvalidCursor})
		return
	}
	if err != nil {
		transport.Error(c, &domain.RuleError{Kind: domain.ReconciliationStorage})
		return
	}
	c.JSON(200, gin.H{"success": true, "data": entries, "has_more": more})
}
