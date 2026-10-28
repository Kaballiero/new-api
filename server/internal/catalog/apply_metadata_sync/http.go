package apply_metadata_sync

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/catalog/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	scenario Usecase
	audit    func(*gin.Context, Output)
}

func NewHTTP(scenario Usecase, audit func(*gin.Context, Output)) HTTP {
	return HTTP{scenario: scenario, audit: audit}
}
func (h HTTP) Handle(c *gin.Context) {
	var input Input
	if err := common.DecodeJson(c.Request.Body, &input); err != nil || len(input.Selections) == 0 || input.SourceVersion == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Preview and select metadata changes before applying"})
		return
	}
	out, err := h.scenario.Execute(c.Request.Context(), input)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	h.audit(c, *out)
	common.ApiSuccess(c, out)
}
