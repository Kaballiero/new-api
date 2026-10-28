package sync_options

import "github.com/QuantumNous/new-api/internal/settings/domain"

type Store interface {
	All() ([]domain.Option, error)
}
type Publisher struct {
	Lock                func() func()
	Apply               func(string, string) error
	CanonicalGroupRatio func(string) string
}
type Diagnostics interface {
	Error(string)
	Log(string)
}
type Usecase struct {
	store       Store
	publisher   Publisher
	diagnostics Diagnostics
}

func NewUsecase(store Store, publisher Publisher, diagnostics Diagnostics) Usecase {
	return Usecase{store, publisher, diagnostics}
}
func (u Usecase) Execute() error {
	unlock := u.publisher.Lock()
	defer unlock()
	options, err := u.store.All()
	if err != nil {
		u.diagnostics.Error("failed to load options from database: " + err.Error())
		return err
	}
	present := make(map[string]bool, len(options))
	for _, option := range options {
		present[option.Key] = true
	}
	for _, option := range options {
		canonical := u.publisher.CanonicalGroupRatio(option.Key)
		if canonical != "" && option.Key != canonical && present[canonical] {
			continue
		}
		if err := u.publisher.Apply(option.Key, option.Value); err != nil {
			u.diagnostics.Log("failed to update option map: " + err.Error())
		}
	}
	return nil
}
