package create_model

import "github.com/QuantumNous/new-api/internal/catalog/domain"

type Input struct{ Value domain.Model }
type Output = domain.Model

type Request domain.Model
