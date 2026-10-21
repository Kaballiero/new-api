package model

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	clearbinding "github.com/QuantumNous/new-api/internal/accounts/clear_binding"
	accountdomain "github.com/QuantumNous/new-api/internal/accounts/domain"
	persist "github.com/QuantumNous/new-api/internal/accounts/persist_account"
	register "github.com/QuantumNous/new-api/internal/accounts/register_account"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
	"strings"
)

// RegistrationOperation registers user; lang is the language of the acting
// request for the persisted registration and invitation log texts.
type RegistrationOperation struct {
	user *User
	lang string
	deps RegistrationDependencies
}

func (op RegistrationOperation) Transaction(run func(register.Mutation) error) error {
	return op.deps.DB().Transaction(func(tx *gorm.DB) error {
		return op.deps.WithEmailLock(tx, op.user.Email, func(tx *gorm.DB) error {
			return run(registrationMutation{user: op.user, tx: tx, ensureEmail: op.deps.EnsureEmail})
		})
	})
}

type registrationMutation struct {
	ensureEmail func(*gorm.DB, string, int) error
	user        *User
	tx          *gorm.DB
}

func (m registrationMutation) NormalizeEmail() {
	m.user.Email = strings.ToLower(strings.TrimSpace(m.user.Email))
}
func (m registrationMutation) EnsureEmail() error {
	return m.ensureEmail(m.tx, m.user.Email, 0)
}

func (m registrationMutation) Password() string { return m.user.Password }
func (m registrationMutation) HashPassword() error {
	var err error
	m.user.Password, err = common.HashAccountPassword(m.user.Password)
	return err
}

func (m registrationMutation) ApplyInitial(initial accountdomain.InitialAccount) {
	m.user.Quota = initial.Quota
	m.user.AffCode = initial.AffiliateCode
}

func (m registrationMutation) SetQuota(quota int)           { m.user.Quota = quota }
func (m registrationMutation) SetAffiliateCode(code string) { m.user.AffCode = code }
func (m registrationMutation) SettingsEmpty() bool          { return m.user.Setting == "" }
func (m registrationMutation) InitializeSettings()          { m.user.SetSetting(dto.UserSetting{}) }
func (m registrationMutation) Insert() error                { return m.tx.Create(m.user).Error }
func (op RegistrationOperation) LoadSidebar(oauth bool) (register.SidebarAccount, error) {
	var user User
	var query *gorm.DB
	if oauth {
		query = op.deps.DB().Where("id = ?", op.user.Id)
	} else {
		query = op.deps.DB().Where("username = ?", op.user.Username)
	}
	if err := query.First(&user).Error; err != nil {
		return nil, err
	}
	return registrationSidebar{user: &user, sidebar: op.deps.Sidebar, save: op.deps.SaveSidebar}, nil
}

type registrationSidebar struct {
	user    *User
	sidebar func(int) string
	save    func(*User)
}

func (s registrationSidebar) DefaultSidebar() string {
	return s.sidebar(s.user.Role)
}

func (s registrationSidebar) AssignSidebar(sidebar string) {
	setting := s.user.GetSetting()
	setting.SidebarModules = sidebar
	s.user.SetSetting(setting)
}

func (s registrationSidebar) SaveSidebar() { s.save(s.user) }
func (s registrationSidebar) LogSidebar() {
	common.SysLog(fmt.Sprintf("initialized sidebar configuration for new user %s (role: %d)", s.user.Username, s.user.Role))
}

func (op RegistrationOperation) NewAffiliateCode() string { return common.GetRandomString(4) }
func (op RegistrationOperation) NewUserQuota() int        { return common.QuotaForNewUser }
func (op RegistrationOperation) RecordRegistrationQuota(quota int) {
	op.deps.RecordSystemLog(op.user.Id, i18n.Translate(op.lang, i18n.MsgAccountRegisterLog, map[string]any{"Quota": logger.LogQuota(quota)}))
}

func (op RegistrationOperation) PaymentComplianceConfirmed() bool {
	return operation_setting.IsPaymentComplianceConfirmed()
}

func (op RegistrationOperation) InviteeQuota() int { return common.QuotaForInvitee }
func (op RegistrationOperation) IncreaseInviteeQuota(quota int) error {
	return op.deps.IncreaseUserQuota(op.user.Id, quota, true)
}

func (op RegistrationOperation) RecordInviteeQuota(quota int) {
	op.deps.RecordSystemLog(op.user.Id, i18n.Translate(op.lang, i18n.MsgAccountInviteeLog, map[string]any{"Quota": logger.LogQuota(quota)}))
}

func (op RegistrationOperation) InviterQuota() int { return common.QuotaForInviter }
func (op RegistrationOperation) RecordInviterQuota(id, quota int) {
	op.deps.RecordSystemLog(id, i18n.Translate(op.lang, i18n.MsgAccountInviterLog, map[string]any{"Quota": logger.LogQuota(quota)}))
}

func (op RegistrationOperation) RewardInviter(id int) error { return op.deps.InviteUser(id) }

type PersistenceOperation struct {
	deps PersistenceDependencies
	user *User
	edit bool
}

func (op PersistenceOperation) PreviousAuthVersion() (int64, error) {
	var previous int64
	err := op.deps.DB().Model(&User{}).Where("id = ?", op.user.Id).Select("auth_version").Find(&previous).Error
	return previous, err
}

func (op PersistenceOperation) Transaction(run func(persist.Mutation) error) error {
	return op.deps.DB().Transaction(func(tx *gorm.DB) error {
		return run(&persistenceMutation{user: op.user, tx: tx, edit: op.edit, advance: op.deps.AdvanceAuthVersion, lock: op.deps.LockForUpdate})
	})
}

func (op PersistenceOperation) UpdateCache() error { return op.deps.UpdateCache() }
func (op PersistenceOperation) AuthVersion() int64 { return op.user.AuthVersion }
func (op PersistenceOperation) RevokeSessions(reason string) error {
	return op.deps.RevokeSessions(reason)
}

type persistenceMutation struct {
	advance            func(*gorm.DB, int) (int64, error)
	lock               func(*gorm.DB) *gorm.DB
	user               *User
	tx                 *gorm.DB
	edit, password     bool
	requested, current User
	updates            map[string]any
}

func (m *persistenceMutation) HashPassword() error {
	m.password = true
	var err error
	m.user.Password, err = common.HashAccountPassword(m.user.Password)
	return err
}

func (m *persistenceMutation) Prepare() {
	m.requested = *m.user
	if m.edit {
		m.updates = map[string]any{"username": m.requested.Username, "display_name": m.requested.DisplayName, "group": m.requested.Group, "remark": m.requested.Remark}
		if m.password {
			m.updates["password"] = m.requested.Password
		}
	}
}

func (m *persistenceMutation) LoadCurrent() (persist.Security, error) {
	query := m.tx
	if !m.edit {
		query = m.lock(query)
	}
	err := query.First(&m.current, m.user.Id).Error
	return persistenceSecurity(m.current), err
}

func persistenceSecurity(user User) persist.Security {
	return persist.Security{Password: user.Password, Role: user.Role, Status: user.Status, Group: user.Group}
}

func (m *persistenceMutation) RequestedSecurity() persist.Security {
	return persistenceSecurity(m.requested)
}

func (m *persistenceMutation) AdvanceAuthVersion() error {
	version, err := m.advance(m.tx, m.user.Id)
	if err != nil {
		return err
	}
	m.requested.AuthVersion = version
	return nil
}

func (m *persistenceMutation) Save() error {
	if m.edit {
		return m.tx.Model(&m.current).Updates(m.updates).Error
	}
	return m.tx.Model(&m.current).Omit("access_token", "access_token_created_at", "quota", "used_quota", "request_count", "aff_count", "aff_quota", "aff_history", "auth_version").Updates(m.requested).Error
}

func (m *persistenceMutation) Reload() error { return m.tx.First(m.user, m.user.Id).Error }

type BindingOperation struct {
	user     *User
	db       func() *gorm.DB
	release  func(*gorm.DB, string, int) error
	telegram string
	cache    func() error
}

func (op BindingOperation) UserID() int { return op.user.Id }
func (op BindingOperation) Transaction(run func(clearbinding.Mutation) error) error {
	return op.db().Transaction(func(tx *gorm.DB) error {
		return run(bindingMutation{user: op.user, tx: tx, release: op.release, telegram: op.telegram})
	})
}

func (op BindingOperation) Reload() error {
	return op.db().Where("id = ?", op.user.Id).First(op.user).Error
}

func (op BindingOperation) UpdateCache() error { return op.cache() }

type bindingMutation struct {
	release  func(*gorm.DB, string, int) error
	telegram string
	user     *User
	tx       *gorm.DB
}

func (m bindingMutation) ClearColumn(column string) error {
	return m.tx.Model(&User{}).Where("id = ?", m.user.Id).Update(column, "").Error
}

func (m bindingMutation) TelegramProvider() string { return m.telegram }
func (m bindingMutation) ReleaseTelegram() error {
	return m.release(m.tx, m.telegram, m.user.Id)
}

type RegistrationDependencies struct {
	DB                func() *gorm.DB
	EnsureEmail       func(*gorm.DB, string, int) error
	WithEmailLock     func(*gorm.DB, string, func(*gorm.DB) error) error
	Sidebar           func(int) string
	SaveSidebar       func(*User)
	RecordSystemLog   func(int, string)
	IncreaseUserQuota func(int, int, bool) error
	InviteUser        func(int) error
}

func NewRegistrationOperation(user *User, lang string, deps RegistrationDependencies) RegistrationOperation {
	return RegistrationOperation{user: user, lang: lang, deps: deps}
}
func NewRegistrationMutation(user *User, tx *gorm.DB, ensure func(*gorm.DB, string, int) error) register.Mutation {
	return registrationMutation{user: user, tx: tx, ensureEmail: ensure}
}

type PersistenceDependencies struct {
	DB                 func() *gorm.DB
	LockForUpdate      func(*gorm.DB) *gorm.DB
	AdvanceAuthVersion func(*gorm.DB, int) (int64, error)
	UpdateCache        func() error
	RevokeSessions     func(string) error
}

func NewPersistenceOperation(user *User, edit bool, deps PersistenceDependencies) PersistenceOperation {
	return PersistenceOperation{user: user, edit: edit, deps: deps}
}
func NewPersistenceMutation(user *User, tx *gorm.DB, password, edit bool, advance func(*gorm.DB, int) (int64, error), lock func(*gorm.DB) *gorm.DB) persist.Mutation {
	return &persistenceMutation{user: user, tx: tx, password: password, edit: edit, advance: advance, lock: lock}
}
func NewBindingOperation(user *User, db func() *gorm.DB, release func(*gorm.DB, string, int) error, telegram string, cache func() error) BindingOperation {
	return BindingOperation{user: user, db: db, release: release, telegram: telegram, cache: cache}
}
