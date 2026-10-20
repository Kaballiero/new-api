package get_account_models

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

func (h *HTTP) Handle(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		id = c.GetInt("id")
	}
	models, err := NewUsecase(h.reader, accountModelSelection{context: c, groups: h.groups, enabled: h.enabled}).Execute(Input{UserID: id})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    models,
	})
}

type HTTP struct {
	reader  ModelAccountGroupReader
	groups  domain.Groups
	enabled func([]string) []string
}

func NewHTTP(reader ModelAccountGroupReader, groups domain.Groups, enabled func([]string) []string) *HTTP {
	return &HTTP{reader: reader, groups: groups, enabled: enabled}
}

type accountModelSelection struct {
	context *gin.Context
	groups  domain.Groups
	enabled func([]string) []string
}

func (s accountModelSelection) ModelGroups(group string) []string {
	return s.groups.ModelGroups(group, s.context.Query("group"))
}
func (s accountModelSelection) EnabledModels(groups []string) []string { return s.enabled(groups) }
