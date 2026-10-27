package wechat

import (
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	accountstore "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/core"
	accountrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	identitystore "github.com/QuantumNous/new-api/internal/adapter/gorm/identity"
	"github.com/QuantumNous/new-api/internal/identity/domain"

	"gorm.io/gorm"
	"net/http"
	"net/url"
	"strconv"
	"time"

	emailadapter "github.com/QuantumNous/new-api/internal/adapter/identity/emailbinding"
	"github.com/gin-gonic/gin"
)

type loginResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    string `json:"data"`
}

func GetIDByCode(code string) (string, error) {
	if code == "" {
		return "", domain.ErrWeChatCodeMissing
	}
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/api/wechat/user?code=%s", common.WeChatServerAddress, url.QueryEscape(code)), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", common.WeChatServerToken)
	client := http.Client{
		Timeout: 5 * time.Second,
	}
	httpResponse, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer httpResponse.Body.Close()
	var res loginResponse
	err = common.DecodeJson(httpResponse.Body, &res)
	if err != nil {
		return "", err
	}
	if !res.Success {
		return "", errors.New(res.Message)
	}
	if res.Data == "" {
		return "", domain.ErrWeChatCodeInvalid
	}
	return res.Data, nil
}

type Login struct {
	InsertUser func(*accountrecord.User, int, string) error

	Accounts accountstore.Store

	Complete func(*accountrecord.User, *gin.Context)
	User     *accountrecord.User
	id       string
}

func (*Login) Enabled() bool { return common.WeChatAuthEnabled }
func (op *Login) ExchangeCode(code string) error {
	var err error
	op.id, err = GetIDByCode(code)
	return err
}
func (op *Login) InitializeAccount()     { op.User = &accountrecord.User{WeChatId: op.id} }
func (op *Login) Taken() bool            { return op.Accounts.IsWeChatIdAlreadyTaken(op.id) }
func (op *Login) LoadAccount() error     { return op.Accounts.FillUserByWeChatId(op.User) }
func (op *Login) AccountID() int         { return op.User.Id }
func (*Login) RegistrationEnabled() bool { return common.RegisterEnabled }
func (op *Login) PrepareAccount() {
	op.User.Username = "wechat_" + strconv.Itoa(op.Accounts.GetMaxUserId()+1)
	op.User.DisplayName = "WeChat User"
	op.User.Role = common.RoleCommonUser
	op.User.Status = common.UserStatusEnabled
}
func (op *Login) InsertAccount(lang string) error { return op.InsertUser(op.User, 0, lang) }
func (op *Login) AccountEnabled() bool            { return op.User.Status == common.UserStatusEnabled }
func (op *Login) CompleteLogin(c *gin.Context)    { op.Complete(op.User, c) }

type Binding struct {
	Accounts accountstore.Store

	SQL identitystore.Store

	Identity           domain.AuthIdentity
	User               *accountrecord.User
	id                 string
	succeeded, warning bool
}

func (op *Binding) ExchangeCode(code string) error {
	var err error
	op.id, err = GetIDByCode(code)
	return err
}
func (op *Binding) Taken() bool { return op.Accounts.IsWeChatIdAlreadyTaken(op.id) }
func (op *Binding) BindAtomically() error {
	return op.SQL.Transaction(func(tx *gorm.DB) error {
		return op.SQL.UpdateUserBindColumnForSessionWithTx(tx, op.Identity, "wechat_id", op.id)
	})
}
func (op *Binding) MarkSucceeded() { op.succeeded = true }
func (op *Binding) LoadAccount() error {
	var err error
	op.User, err = op.Accounts.GetUserById(op.Identity.UserID, false)
	return err
}
func (op *Binding) NotifyAccount() bool {
	return emailadapter.NotifyAccountSecurityChange(op.User.Email, "WeChat account linked") != nil
}
func (op *Binding) SetNotificationWarning(warning bool) { op.warning = warning }
func (op *Binding) Succeeded() bool                     { return op.succeeded }
func (op *Binding) NotificationWarning() bool           { return op.warning }
