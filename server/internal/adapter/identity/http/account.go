package identityhttp

import (
	"github.com/QuantumNous/new-api/common"
	accountstore "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/core"
	accountrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	"github.com/QuantumNous/new-api/internal/identity/domain"

	"github.com/gin-gonic/gin"
)

func GetAuthenticatedUser(accounts accountstore.Store, c *gin.Context) (*accountrecord.User, error) {
	id := c.GetInt("id")
	if id == 0 {
		return nil, domain.ErrAuthTokenInvalid
	}
	user, err := accounts.GetUserById(id, false)
	if err != nil {
		return nil, err
	}
	if user.Status != common.UserStatusEnabled {
		return nil, domain.ErrSessionInactive
	}
	return user, nil
}
