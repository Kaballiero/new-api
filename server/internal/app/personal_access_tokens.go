package app

import (
	"github.com/QuantumNous/new-api/common"
	identityhttp "github.com/QuantumNous/new-api/internal/adapter/identity/http"
	"github.com/QuantumNous/new-api/internal/personal_access_tokens/domain"
	"github.com/QuantumNous/new-api/internal/personal_access_tokens/generate_token"
	"github.com/QuantumNous/new-api/internal/personal_access_tokens/get_status"
	"github.com/QuantumNous/new-api/internal/personal_access_tokens/revoke_token"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type PersonalAccessTokensHTTP struct {
	Status   get_status.HTTP
	Generate generate_token.HTTP
	Revoke   revoke_token.HTTP
}

func NewPersonalAccessTokensHTTP(audit func(*gin.Context, int, string, map[string]any)) PersonalAccessTokensHTTP {
	store := model.NewPersonalAccessTokenPersistence()
	roles := domain.AuditRoles{Root: common.RoleRootUser, Common: common.RoleCommonUser, Admin: common.RoleAdminUser}
	return PersonalAccessTokensHTTP{
		Status: get_status.NewHTTP(store, domain.Fingerprint, roles, identityhttp.WriteSecurityOperationError),
		Generate: generate_token.NewHTTP(store, func(c *gin.Context) bool {
			return middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeAccessTokenGenerate}) != nil
		}, func() (string, error) { return common.GenerateRandomKey(29 + common.GetRandomInt(4)) }, domain.Fingerprint, func(c *gin.Context, id int, ref string) {
			audit(c, id, "access_token.generate", map[string]any{"token_ref": ref})
		}, identityhttp.WriteSecurityOperationError),
		Revoke: revoke_token.NewHTTP(store, func(c *gin.Context) bool {
			return middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeAccessTokenRevoke}) != nil
		}, domain.Fingerprint, func(c *gin.Context, id int, ref string) {
			audit(c, id, "access_token.revoke", map[string]any{"token_ref": ref})
		}, identityhttp.WriteSecurityOperationError),
	}
}
