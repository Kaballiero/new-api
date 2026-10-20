package update_profile

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
	"net/http"
)

type Decoder interface {
	DecodeAccount(map[string]any) (AccountInput, SelfUpdateIssue)
}
type HTTP struct {
	store              SelfUpdateStore
	decoder            Decoder
	passwords          func(*gin.Context, *bool) PasswordChanger
	audit              func(*gin.Context, bool, bool)
	writeSecurityError func(*gin.Context, error)
}

func NewHTTP(store SelfUpdateStore, decoder Decoder, passwords func(*gin.Context, *bool) PasswordChanger, audit func(*gin.Context, bool, bool), writeSecurityError func(*gin.Context, error)) *HTTP {
	return &HTTP{store: store, decoder: decoder, passwords: passwords, audit: audit, writeSecurityError: writeSecurityError}
}

type requestUser struct{ context *gin.Context }

func (u requestUser) UserID() int { return u.context.GetInt("id") }

type accountInput struct {
	request map[string]any
	decoder Decoder
}

func (r accountInput) ReadAccount() (AccountInput, SelfUpdateIssue) {
	return r.decoder.DecodeAccount(r.request)
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
	result, password := NewUsecase(h.store, accountInput{request: request, decoder: h.decoder}, h.passwords(c, &succeeded)).Execute(request, requestUser{context: c})
	notificationFailed = password.NotificationFailed
	switch result.Issue {
	case SelfUpdatePreferencesFailed:
		common.ApiErrorI18n(c, i18n.MsgUpdateFailed)
		return
	case SelfUpdateLoadFailed:
		common.ApiError(c, result.Err)
		return
	case InvalidParameters:
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	case InvalidInput:
		common.ApiErrorI18n(c, i18n.MsgInvalidInput)
		return
	case SecurityFailure:
		h.writeSecurityError(c, result.Err)
		return
	}
	if result.Preferences {
		common.ApiSuccessI18n(c, i18n.MsgUpdateSuccess, nil)
		return
	}
	if password.ProofRejected {
		return
	}
	if password.Data != nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": password.Data})
		return
	}
	if result.Err != nil {
		common.ApiError(c, result.Err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}
