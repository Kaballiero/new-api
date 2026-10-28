package update_vendor

import "github.com/QuantumNous/new-api/internal/catalog/domain"

type Input struct{ Value domain.Vendor }
type Output = domain.Vendor

type Request domain.Vendor
