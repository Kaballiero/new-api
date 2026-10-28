package update_key

import (
	"github.com/QuantumNous/new-api/internal/apikeys/domain"
	"github.com/QuantumNous/new-api/internal/apikeys/dto"
)

type Input struct {
	UserID        int
	StatusOnly    string
	Token         domain.Token
	AutoGroups    []string
	AutoGroupsSet bool
}
type Request = dto.Request
type Response = dto.Response
