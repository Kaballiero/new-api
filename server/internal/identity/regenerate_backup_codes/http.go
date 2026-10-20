package regenerate_backup_codes

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/gin-gonic/gin"
	"net/http"
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
	failure, err := NewUsecase(op).Execute()
	if err != nil {
		if failure == GenerationFailed || failure == ReplacementFailed {
			message := "生成备用码失败"
			if failure == ReplacementFailed {
				message = "保存备用码失败"
			}
			c.JSON(http.StatusOK, gin.H{"success": false, "message": message})
			common.SysLog(message + ": " + err.Error())
			return
		}
		h.writeError(c, err)
		return
	}
	h.audit(c, identity.UserID, "user.2fa_backup_codes", nil)
	data := h.rotation(op.Bundle())
	data["backup_codes"] = op.BackupCodes()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "备用码重新生成成功", "data": data})
}
