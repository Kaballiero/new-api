package update_pricing_config

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/gin-gonic/gin"
)

type HTTP struct {
	store               Store
	policy              *domain.Configuration
	log                 func(string)
	refresh, invalidate func()
	audit               func(*gin.Context, []string)
}

func NewHTTP(store Store, policy *domain.Configuration, log func(string), refresh, invalidate func(), audit func(*gin.Context, []string)) HTTP {
	return HTTP{store: store, policy: policy, log: log, refresh: refresh, invalidate: invalidate, audit: audit}
}
func (h HTTP) Handle(c *gin.Context) {
	var request Request
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	audit := func(names []string) { h.audit(c, names) }
	if err := NewUsecase(h.store, h.policy, h.log, h.refresh, h.invalidate, audit).Execute(request.Changes); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, domain.ErrModelPricingConflict) {
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
