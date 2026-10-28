package report_system_instance

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/internal/system/domain"
)

type Facts interface {
	Identity() (domain.NodeIdentity, string, error)
	Snapshot(domain.NodeIdentity, string) domain.SystemInstanceInfo
}
type Store interface {
	UpsertSystemInstance(string, any, int64, int64) error
}
type Usecase struct {
	facts Facts
	store Store
	now   func() int64
	start func() int64
}

func NewUsecase(facts Facts, store Store, now, start func() int64) Usecase {
	return Usecase{facts: facts, store: store, now: now, start: start}
}
func (u Usecase) Execute() error {
	identity, hostname, hostnameErr := u.facts.Identity()
	if strings.TrimSpace(identity.Name) == "" {
		if hostnameErr != nil || strings.TrimSpace(hostname) == "" {
			return fmt.Errorf("system instance node name is empty")
		}
		identity.Name = hostname
		identity.Source = "hostname"
		identity.ManuallyConfigured = false
		identity.ShouldConfigureManually = true
	}
	info := u.facts.Snapshot(identity, hostname)
	return u.store.UpsertSystemInstance(identity.Name, info, u.start(), u.now())
}
