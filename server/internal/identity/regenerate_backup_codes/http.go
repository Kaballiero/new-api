package regenerate_backup_codes

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/gin-gonic/gin"
)

type Completion interface {
	Operation
	Bundle() *dto.AuthBundle
	BackupCodes() []string
}
type HTTP struct {
	authorize  func(*gin.Context) (domain.AuthIdentity, bool)
	operation  func(Input) Completion
	writeError func(*gin.Context, error)
	audit      func(*gin.Context, int, string, map[string]any)
	rotation   func(*dto.AuthBundle) gin.H
}

func NewHTTP(authorize func(*gin.Context) (domain.AuthIdentity, bool), operation func(Input) Completion, writeError func(*gin.Context, error), audit func(*gin.Context, int, string, map[string]any), rotation func(*dto.AuthBundle) gin.H) *HTTP {
	return &HTTP{authorize: authorize, operation: operation, writeError: writeError, audit: audit, rotation: rotation}
}
func (h *HTTP) Handle(c *gin.Context) {
	identity, ok := h.authorize(c)
	if !ok {
		return
	}
	op := h.operation(Input{Identity: identity})
	if err := NewUsecase(op).Execute(); err != nil {
		var failed *domain.LoggedError
		if !errors.As(err, &failed) {
			h.writeError(c, err)
			return
		}
		// The response message is localized; the system log is in English.
		key, logPrefix := i18n.MsgIdentityBackupCodesGenerateFailed, "failed to generate backup codes"
		if errors.Is(err, domain.ErrBackupCodesSave) {
			key, logPrefix = i18n.MsgIdentityBackupCodesSaveFailed, "failed to save backup codes"
		}
		common.ApiErrorI18n(c, key)
		common.SysLog(logPrefix + ": " + failed.Log)
		return
	}
	h.audit(c, identity.UserID, "user.2fa_backup_codes", nil)
	data := h.rotation(op.Bundle())
	data["backup_codes"] = op.BackupCodes()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": common.TranslateMessage(c, i18n.MsgIdentityBackupCodesRegenerated), "data": data})
}
