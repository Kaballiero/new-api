package get_vendor

import (
	"github.com/QuantumNous/new-api/internal/catalog/domain"
)

type Input struct{ ID int }
type Output = domain.Vendor
