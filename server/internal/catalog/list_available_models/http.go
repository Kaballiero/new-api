package list_available_models

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/catalog/domain/availability"
	"github.com/gin-gonic/gin"
)

type HTTP struct {
	scenario   Usecase
	autoGroups func(*gin.Context, string) []string
	write      func(*gin.Context, int, []availability.Model)
}

func NewHTTP(scenario Usecase, autoGroups func(*gin.Context, string) []string, write func(*gin.Context, int, []availability.Model)) HTTP {
	return HTTP{scenario: scenario, autoGroups: autoGroups, write: write}
}
func (h HTTP) Handle(c *gin.Context, format int) {
	enabled := common.GetContextKeyBool(c, constant.ContextKeyTokenModelLimitEnabled)
	var limits map[string]bool
	if enabled {
		if value, ok := common.GetContextKey(c, constant.ContextKeyTokenModelLimit); ok {
			limits, _ = value.(map[string]bool)
		}
		if limits == nil {
			limits = map[string]bool{}
		}
	}
	out, err := h.scenario.Execute(Input{UserID: c.GetInt("id"), UserGroup: common.GetContextKeyString(c, constant.ContextKeyUserGroup), TokenGroup: common.GetContextKeyString(c, constant.ContextKeyTokenGroup), LimitEnabled: enabled, Limits: limits, AutoGroups: func(group string) []string { return h.autoGroups(c, group) }})
	if err != nil {
		c.JSON(200, gin.H{"success": false, "message": err.Error()})
		return
	}
	h.write(c, format, out)
}
