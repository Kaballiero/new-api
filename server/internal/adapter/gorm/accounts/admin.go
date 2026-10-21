package accountsadapter

import (
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/internal/accounts/create_account"
	"github.com/QuantumNous/new-api/internal/accounts/manage_account"
	"github.com/QuantumNous/new-api/internal/accounts/update_account"
	"github.com/QuantumNous/new-api/model"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/accounts/domain"

	"gorm.io/gorm"
)

// AdminHooks подключает эффекты хоста без зависимости от authorization и прежней модели.
// Все обратные вызовы обязательны; фабрики лишь сохраняют функции и не обращаются к хранилищу.
type AdminHooks struct {
	SetPermissionsWithTx     func(*gorm.DB, int, map[string]map[string]bool) error
	ClearAuthorizationWithTx func(*gorm.DB, int) error
	Reload                   func() error
	Revoke                   func(int, string) error
	Publish                  func(int) error
	Invalidate               func(int) error
}
type adminEffects struct{ hooks AdminHooks }

func (op adminEffects) ReloadPolicy() error { return op.hooks.Reload() }
func (op adminEffects) RevokeSessions(id int, reason string) error {
	return op.hooks.Revoke(id, reason)
}
func (op adminEffects) PublishAuthCache(id int) error { return op.hooks.Publish(id) }
func adminTarget(user *model.User) domain.AdminTarget {
	return domain.AdminTarget{ID: user.Id, Username: user.Username, Role: user.Role, Status: user.Status, AuthVersion: user.AuthVersion}
}

// adminLoadError classifies a failed account load for the admin scenarios.
func adminLoadError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.ErrAccountNotFound
	}
	return domain.Internal(err)
}

type AdminCreateOperation struct {
	adminEffects
	user  *model.User
	audit func(domain.AdminTarget)
}

func NewAdminCreateOperation(hooks AdminHooks, audit func(domain.AdminTarget)) *AdminCreateOperation {
	return &AdminCreateOperation{adminEffects: adminEffects{hooks: hooks}, audit: audit}
}
func (op *AdminCreateOperation) CreateTransaction(run func(create_account.Mutation) error) error {
	return model.DB.Transaction(func(tx *gorm.DB) error { return run(adminCreateMutation{op: op, tx: tx}) })
}
func (op *AdminCreateOperation) FinishInsert()                          { op.user.FinishInsert(0) }
func (op *AdminCreateOperation) AuditCreated(target domain.AdminTarget) { op.audit(target) }

type adminCreateMutation struct {
	op *AdminCreateOperation
	tx *gorm.DB
}

func (m adminCreateMutation) Insert(input create_account.NewAccount) (domain.AdminTarget, error) {
	m.op.user = &model.User{Username: input.Username, Password: input.Password, DisplayName: input.DisplayName, Role: input.Role}
	err := m.op.user.InsertWithTx(m.tx, 0)
	return adminTarget(m.op.user), err
}
func (m adminCreateMutation) SetPermissions(id int, p map[string]map[string]bool) error {
	return m.op.hooks.SetPermissionsWithTx(m.tx, id, p)
}
func (m adminCreateMutation) ClearAuthorization(id int) error {
	return m.op.hooks.ClearAuthorizationWithTx(m.tx, id)
}

type AdminEditOperation struct {
	adminEffects
	request *model.User
	origin  *model.User
	audit   func(domain.AdminTarget)
}

func NewAdminEditOperation(request *domain.User, hooks AdminHooks, audit func(domain.AdminTarget)) *AdminEditOperation {
	return &AdminEditOperation{adminEffects: adminEffects{hooks: hooks}, request: adminRequestRecord(request), audit: audit}
}
func (op *AdminEditOperation) LoadOrigin(id int) (domain.AdminTarget, error) {
	user, err := model.GetUserById(id, false)
	if err != nil {
		return domain.AdminTarget{}, adminLoadError(err)
	}
	op.origin = user
	return adminTarget(user), nil
}
func (op *AdminEditOperation) UseOriginRole(role int) { op.request.Role = role }

// EditTransaction reports a taken username as domain.ErrUsernameTaken; the
// model error is read at call time.
func (op *AdminEditOperation) EditTransaction(run func(update_account.Mutation) error) error {
	err := model.DB.Transaction(func(tx *gorm.DB) error { return run(adminEditMutation{op: op, tx: tx}) })
	if errors.Is(err, model.ErrUsernameAlreadyTaken) {
		return domain.ErrUsernameTaken
	}
	return err
}
func (op *AdminEditOperation) AuditUpdated(target domain.AdminTarget) { op.audit(target) }
func (op *AdminEditOperation) ReloadAccount(id int) (*domain.User, error) {
	user, err := model.GetUserById(id, false)
	return adminQueryUser(user), err
}
func (op *AdminEditOperation) RequestedAccount() *domain.User { return adminQueryUser(op.request) }

type adminEditMutation struct {
	op *AdminEditOperation
	tx *gorm.DB
}

func (m adminEditMutation) EnsureUsernameAvailable(username string, id int) error {
	return model.EnsureUsernameAvailableWithTx(m.tx, username, id)
}
func (m adminEditMutation) Edit(password bool) (int64, error) {
	err := m.op.request.EditWithTx(m.tx, password)
	return m.op.request.AuthVersion, err
}
func (m adminEditMutation) SetPermissions(id int, p map[string]map[string]bool) error {
	return m.op.hooks.SetPermissionsWithTx(m.tx, id, p)
}
func (m adminEditMutation) ClearAuthorization(id int) error {
	return m.op.hooks.ClearAuthorizationWithTx(m.tx, id)
}

type AdminManageOperation struct {
	adminEffects
	user  model.User
	audit func(domain.AdminTarget, string)
}

func NewAdminManageOperation(hooks AdminHooks, audit func(domain.AdminTarget, string)) *AdminManageOperation {
	return &AdminManageOperation{adminEffects: adminEffects{hooks: hooks}, audit: audit}
}
func (op *AdminManageOperation) LoadUnscoped(id int) (domain.AdminTarget, error) {
	err := model.DB.Unscoped().Where("id = ?", id).First(&op.user).Error
	return adminTarget(&op.user), adminLoadError(err)
}
func (op *AdminManageOperation) UseRole(role int)     { op.user.Role = role }
func (op *AdminManageOperation) UseStatus(status int) { op.user.Status = status }
func (op *AdminManageOperation) Update() (domain.AdminTarget, error) {
	err := op.user.Update(false)
	return adminTarget(&op.user), err
}
func (op *AdminManageOperation) DemoteTransaction(run func(manage_account.DemoteMutation) error) error {
	return model.DB.Transaction(func(tx *gorm.DB) error { return run(adminDemoteMutation{op: op, tx: tx}) })
}
func (op *AdminManageOperation) Delete() error                 { return op.user.Delete() }
func (op *AdminManageOperation) InvalidateTokens(id int) error { return op.hooks.Invalidate(id) }
func (op *AdminManageOperation) LogTokenInvalidationFailure(id int, err error) {
	common.SysLog(fmt.Sprintf("failed to invalidate tokens cache for user %d: %s", id, err.Error()))
}
func (op *AdminManageOperation) AuditManaged(target domain.AdminTarget, action string) {
	op.audit(target, action)
}

type adminDemoteMutation struct {
	op *AdminManageOperation
	tx *gorm.DB
}

func (m adminDemoteMutation) Update() (domain.AdminTarget, error) {
	err := m.op.user.UpdateWithTx(m.tx, false)
	return adminTarget(&m.op.user), err
}
func (m adminDemoteMutation) ClearAuthorization(id int) error {
	return m.op.hooks.ClearAuthorizationWithTx(m.tx, id)
}

// adminRequestRecord восстанавливает запись из прежних принимаемых полей административного запроса.
func adminRequestRecord(user *domain.User) *model.User {
	if user == nil {
		return nil
	}
	record := &model.User{
		Id:               user.Id,
		Username:         user.Username,
		Password:         user.Password,
		OriginalPassword: user.OriginalPassword,
		DisplayName:      user.DisplayName,
		Role:             user.Role,
		Status:           user.Status,
		Email:            user.Email,
		GitHubId:         user.GitHubId,
		DiscordId:        user.DiscordId,
		OidcId:           user.OidcId,
		WeChatId:         user.WeChatId,
		TelegramId:       user.TelegramId,
		VerificationCode: user.VerificationCode,
		Quota:            user.Quota,
		UsedQuota:        user.UsedQuota,
		RequestCount:     user.RequestCount,
		Group:            user.Group,
		AffCode:          user.AffCode,
		AffCount:         user.AffCount,
		AffQuota:         user.AffQuota,
		AffHistoryQuota:  user.AffHistoryQuota,
		InviterId:        user.InviterId,
		LinuxDOId:        user.LinuxDOId,
		Setting:          user.Setting,
		Remark:           user.Remark,
		StripeCustomer:   user.StripeCustomer,
		CreatedAt:        user.CreatedAt,
		LastLoginAt:      user.LastLoginAt,
		AdminPermissions: user.AdminPermissions,
	}
	if user.DeletedAt != nil {
		record.DeletedAt = gorm.DeletedAt{Time: *user.DeletedAt, Valid: true}
	}
	return record
}
