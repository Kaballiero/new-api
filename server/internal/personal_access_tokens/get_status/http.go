package get_status

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/personal_access_tokens/domain"
	"github.com/QuantumNous/new-api/internal/personal_access_tokens/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	reader             Reader
	fingerprint        func(string) string
	roles              domain.AuditRoles
	writeSecurityError func(*gin.Context, error)
}

func NewHTTP(reader Reader, fingerprint func(string) string, roles domain.AuditRoles, writeSecurityError func(*gin.Context, error)) HTTP {
	return HTTP{reader: reader, fingerprint: fingerprint, roles: roles, writeSecurityError: writeSecurityError}
}
func (h HTTP) Handle(c *gin.Context) {
	status, err := NewUsecase(h.reader, h.fingerprint, h.roles).Execute(Input{UserID: c.GetInt("id")})
	if err != nil {
		transport.WriteError(c, err, h.writeSecurityError)
		return
	}
	common.ApiSuccess(c, status)
}
