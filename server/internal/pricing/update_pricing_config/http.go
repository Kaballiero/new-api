package update_pricing_config

import (
	"errors"
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct {
	usecase  Usecase
	conflict func() error
}

func NewHTTP(usecase Usecase, conflict func() error) HTTP {
	return HTTP{usecase: usecase, conflict: conflict}
}
func (h HTTP) Handle(c *gin.Context) {
	var request Request
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := h.usecase.Execute(request.Changes); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, h.conflict()) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"success": false, "message": err.Error()})
		return
	}
	names := make([]string, 0, len(request.Changes))
	for _, change := range request.Changes {
		names = append(names, change.ModelName)
	}
	common.ApiSuccess(c, Output{UpdatedModels: names})
}
