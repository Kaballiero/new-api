package update_key

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
	reader   Store
	groups   func(*gin.Context) domain.AutoGroupRules
	maxQuota func() int
	now      func() int64
	audit    func(*gin.Context) map[string]any
}

func NewHTTP(reader Store, groups func(*gin.Context) domain.AutoGroupRules, maxQuota func() int, now func() int64, audit func(*gin.Context) map[string]any) HTTP {
	return HTTP{reader: reader, groups: groups, maxQuota: maxQuota, now: now, audit: audit}
}
func (h HTTP) Handle(c *gin.Context) {
	userID := c.GetInt("id")
	statusOnly := c.Query("status_only")
	request := Request{}
	err := c.ShouldBindJSON(&request)
	if err != nil {
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
		return
	}
	cleanToken, err := NewUsecase(h.reader, h.groups(c), h.maxQuota, h.now, func() map[string]any { return h.audit(c) }, func() { common.SetContextKey(c, constant.ContextKeyTokenAuditSucceeded, true) }).Execute(Input{UserID: userID, StatusOnly: statusOnly, Token: request.Token.Domain(), AutoGroups: request.AutoGroups.Groups, AutoGroupsSet: request.AutoGroups.Set})
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccess(c, dto.Masked(cleanToken, domain.MaskKey, common.SysError))
}
