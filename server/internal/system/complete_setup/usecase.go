package complete_setup

import (
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/internal/system/domain"
)

type Root interface {
	Create(string, string) error
}
type Store interface{ CreateSetup(string, int64) error }
type Options interface{ Execute(string, string) error }
type Input struct {
	Username, Password, ConfirmPassword string
	SelfUseModeEnabled, DemoSiteEnabled bool
	RootExists                          bool
}
type Runtime struct {
	SetModes func(bool, bool)
	SetSetup func(bool)
	Version  func() string
	Now      func() int64
}
type Passwords struct {
	Validate func(string) error
	Hash     func(string) (string, error)
}
type Usecase struct {
	root      Root
	store     Store
	options   Options
	runtime   Runtime
	passwords Passwords
}

func NewUsecase(root Root, store Store, options Options, runtime Runtime, passwords Passwords) Usecase {
	return Usecase{root: root, store: store, options: options, runtime: runtime, passwords: passwords}
}
func (u Usecase) Execute(req Input) error {
	if !req.RootExists {
		if len(req.Username) > 12 {
			return domain.ErrSetupUsername
		}
		if req.Password != req.ConfirmPassword {
			return domain.ErrSetupPasswordMismatch
		}
		if err := u.passwords.Validate(req.Password); err != nil {
			return fmt.Errorf("%w: %w", domain.ErrSetupPassword, err)
		}
		hash, err := u.passwords.Hash(req.Password)
		if err != nil {
			return fmt.Errorf("%w: %w", domain.ErrSetupHash, err)
		}
		if err := u.root.Create(req.Username, hash); err != nil {
			return fmt.Errorf("%w: %w", domain.ErrSetupRoot, err)
		}
	}
	u.runtime.SetModes(req.SelfUseModeEnabled, req.DemoSiteEnabled)
	if err := u.options.Execute("SelfUseModeEnabled", strconv.FormatBool(req.SelfUseModeEnabled)); err != nil {
		return fmt.Errorf("%w: %w", domain.ErrSetupSelfMode, err)
	}
	if err := u.options.Execute("DemoSiteEnabled", strconv.FormatBool(req.DemoSiteEnabled)); err != nil {
		return fmt.Errorf("%w: %w", domain.ErrSetupDemoMode, err)
	}
	u.runtime.SetSetup(true)
	if err := u.store.CreateSetup(u.runtime.Version(), u.runtime.Now()); err != nil {
		return fmt.Errorf("%w: %w", domain.ErrSetupPersist, err)
	}
	return nil
}
