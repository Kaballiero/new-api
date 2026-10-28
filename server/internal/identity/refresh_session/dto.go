package refresh_session

import "github.com/QuantumNous/new-api/internal/identity/dto"

type Output struct {
	Bundle  *dto.AuthBundle
	Profile func() map[string]any
}
