package get_catalog_freshness

import (
	"time"

	"github.com/QuantumNous/new-api/internal/pricing/domain"
)

// Tariffs keeps the tariffs of the published upstream catalog revision the
// OpenRouter reserves are priced from.
type Tariffs interface {
	Load() *domain.UpstreamCatalogTariffs
}

type Usecase struct {
	tariffs Tariffs
	now     func() time.Time
	maxAge  func() time.Duration
}

func NewUsecase(tariffs Tariffs, now func() time.Time, maxAge func() time.Duration) Usecase {
	return Usecase{tariffs: tariffs, now: now, maxAge: maxAge}
}

// Execute reports when the catalog data of the published upstream catalog
// last changed and whether it is older than maxAge: a stale catalog no longer
// prices OpenRouter reserves first, live pricing does.
func (u Usecase) Execute() Output {
	maxAge := u.maxAge()
	output := Output{MaxAgeHours: int(maxAge / time.Hour)}
	tariffs := u.tariffs.Load()
	if tariffs == nil {
		return output
	}
	output.FetchedAt = tariffs.FetchedAt.Unix()
	output.Stale = !tariffs.FreshAt(u.now(), maxAge)
	return output
}
