package create_key

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/apikeys/domain"
	"github.com/QuantumNous/new-api/internal/apikeys/dto"
	"github.com/QuantumNous/new-api/internal/apikeys/transport"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct {
	reader              Store
	groups              func(*gin.Context) domain.AutoGroupRules
	maxQuota, maxTokens func() int
	now                 func() int64
	generate            func() (string, error)
	audit               func(*gin.Context) map[string]any
}

func NewHTTP(reader Store, groups func(*gin.Context) domain.AutoGroupRules, maxQuota, maxTokens func() int, now func() int64, generate func() (string, error), audit func(*gin.Context) map[string]any) HTTP {
	return HTTP{reader: reader, groups: groups, maxQuota: maxQuota, maxTokens: maxTokens, now: now, generate: generate, audit: audit}
}
func (h HTTP) Handle(c *gin.Context) {
	request := Request{}
	err := c.ShouldBindJSON(&request)
	if err != nil {
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
		return
	}
	cleanToken, err := NewUsecase(h.reader, h.groups(c), h.maxQuota, h.maxTokens, h.now, h.generate, common.SysLog, func() map[string]any { return h.audit(c) }, func() { common.SetContextKey(c, constant.ContextKeyTokenAuditSucceeded, true) }).Execute(Input{UserID: func() int { return c.GetInt("id") }, Token: request.Token.Domain(), AutoGroups: request.AutoGroups.Groups, AutoGroupsSet: request.AutoGroups.Set})
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccessStatus(c, http.StatusCreated, dto.Masked(cleanToken, domain.MaskKey, common.SysError))
}
