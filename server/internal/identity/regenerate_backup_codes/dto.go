package regenerate_backup_codes

import "github.com/QuantumNous/new-api/internal/identity/domain"

type Input struct{ Identity domain.AuthIdentity }
type Failure uint8

const (
	NoFailure Failure = iota
	GenerationFailed
	ReplacementFailed
	SessionFailed
)
