package update_profile

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/QuantumNous/new-api/internal/accounts/dto"
	"github.com/QuantumNous/new-api/internal/accounts/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	store              SelfUpdateStore
	passwords          func(*gin.Context, *bool) PasswordChanger
	audit              func(*gin.Context, bool, bool)
	writeSecurityError func(*gin.Context, error)
}

func NewHTTP(store SelfUpdateStore, passwords func(*gin.Context, *bool) PasswordChanger, audit func(*gin.Context, bool, bool), writeSecurityError func(*gin.Context, error)) *HTTP {
	return &HTTP{store: store, passwords: passwords, audit: audit, writeSecurityError: writeSecurityError}
}

type requestUser struct{ context *gin.Context }

func (u requestUser) UserID() int { return u.context.GetInt("id") }

// accountInput decodes the request as a full admin account body, so the
// previous validation of all account fields, including unused ones, applies.
type accountInput struct{ request map[string]any }

func (r accountInput) ReadAccount() (AccountInput, error) {
	var user dto.User
	raw, err := common.Marshal(r.request)
	if err != nil {
		return AccountInput{}, domain.ErrProfileInvalid
	}
	if err := common.Unmarshal(raw, &user); err != nil {
		return AccountInput{}, domain.ErrProfileInvalid
	}
	if err := common.Validate.StructExcept(&user, "Password"); err != nil {
		return AccountInput{}, domain.ErrProfileInputFailed
	}
	return AccountInput{Username: user.Username, Password: user.Password, DisplayName: user.DisplayName, OriginalPassword: user.OriginalPassword}, nil
}

func (h *HTTP) Handle(c *gin.Context) {
	var request map[string]any
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	succeeded, notificationFailed := false, false
	if PasswordRequested(request) {
		defer func() { h.audit(c, succeeded, notificationFailed) }()
	}
	result, err := NewUsecase(h.store, accountInput{request: request}, h.passwords(c, &succeeded)).Execute(request, requestUser{context: c})
	notificationFailed = result.Password.NotificationFailed
	if err != nil {
		if errors.Is(err, domain.ErrPasswordChangeFailed) {
			h.writeSecurityError(c, errors.Unwrap(err))
			return
		}
		transport.WriteError(c, err)
		return
	}
	if result.Preferences {
		common.ApiSuccessI18n(c, i18n.MsgUpdateSuccess, nil)
		return
	}
	if result.Password.ProofRejected {
		return
	}
	if result.Password.Data != nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": result.Password.Data})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}
