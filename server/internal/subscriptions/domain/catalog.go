package domain

import payments "github.com/QuantumNous/new-api/internal/payments/domain"

type CatalogSettings interface {
	PancakeCredentials() payments.PancakeCredentials
	PancakeStore() string
	PancakeReturn() string
}
