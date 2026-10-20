package register_account

import "github.com/QuantumNous/new-api/internal/accounts/domain"

type Mutation interface {
	NormalizeEmail()
	EnsureEmail() error
	Password() string
	HashPassword() error
	ApplyInitial(domain.InitialAccount)
	SetQuota(int)
	SetAffiliateCode(string)
	SettingsEmpty() bool
	InitializeSettings()
	Insert() error
}
type Store interface {
	Transaction(func(Mutation) error) error
}
type Effects interface {
	LoadSidebar(bool) (SidebarAccount, error)
	NewUserQuota() int
	NewAffiliateCode() string
	RecordRegistrationQuota(int)
	PaymentComplianceConfirmed() bool
	InviteeQuota() int
	IncreaseInviteeQuota(int) error
	RecordInviteeQuota(int)
	InviterQuota() int
	RecordInviterQuota(int, int)
	RewardInviter(int) error
}
type SidebarAccount interface {
	DefaultSidebar() string
	AssignSidebar(string)
	SaveSidebar()
	LogSidebar()
}
type Usecase struct {
	store   Store
	effects Effects
}

func NewUsecase(store Store, effects Effects) Usecase { return Usecase{store: store, effects: effects} }
func (u Usecase) InsertRecord(initialize func() domain.InitialAccount) error {
	return u.store.Transaction(func(tx Mutation) error { return u.InsertWith(tx, initialize) })
}
func (u Usecase) InsertWith(tx Mutation, initialize func() domain.InitialAccount) error {
	return u.insert(tx, func() { tx.ApplyInitial(initialize()) })
}
func (u Usecase) InsertDefaults(tx Mutation) error {
	return u.insert(tx, func() {
		tx.SetQuota(u.effects.NewUserQuota())
		tx.SetAffiliateCode(u.effects.NewAffiliateCode())
	})
}
func (u Usecase) insert(tx Mutation, initialize func()) error {
	tx.NormalizeEmail()
	if err := tx.EnsureEmail(); err != nil {
		return err
	}
	if tx.Password() != "" {
		if err := tx.HashPassword(); err != nil {
			return err
		}
	}
	initialize()
	if tx.SettingsEmpty() {
		tx.InitializeSettings()
	}
	return tx.Insert()
}
func (u Usecase) Execute(inviterID int, initialize func() domain.InitialAccount) error {
	if err := u.InsertRecord(initialize); err != nil {
		return err
	}
	u.Finish(inviterID, false)
	return nil
}
func (u Usecase) Finish(inviterID int, oauth bool) {
	u.InitializeSidebar(oauth)
	if u.effects.NewUserQuota() > 0 {
		u.effects.RecordRegistrationQuota(u.effects.NewUserQuota())
	}
	if inviterID != 0 && u.effects.PaymentComplianceConfirmed() {
		if u.effects.InviteeQuota() > 0 {
			_ = u.effects.IncreaseInviteeQuota(u.effects.InviteeQuota())
			u.effects.RecordInviteeQuota(u.effects.InviteeQuota())
		}
		if u.effects.InviterQuota() > 0 {
			u.effects.RecordInviterQuota(inviterID, u.effects.InviterQuota())
			_ = u.effects.RewardInviter(inviterID)
		}
	}
}

func (u Usecase) InitializeSidebar(oauth bool) {
	account, err := u.effects.LoadSidebar(oauth)
	if err != nil {
		return
	}
	sidebar := account.DefaultSidebar()
	if sidebar != "" {
		account.AssignSidebar(sidebar)
		account.SaveSidebar()
		account.LogSidebar()
	}
}
