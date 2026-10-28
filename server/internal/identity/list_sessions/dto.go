package list_sessions

import "github.com/QuantumNous/new-api/internal/identity/dto"

type Input struct {
	UserID     int
	CurrentSID string
}
type Output = []dto.LoginSessionView
