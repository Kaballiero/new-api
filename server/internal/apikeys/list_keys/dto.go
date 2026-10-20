package list_keys

import "github.com/QuantumNous/new-api/internal/apikeys/domain"

type Input struct {
	UserID, Start, Limit int
	Keyword, Key         string
}
type Output struct {
	Tokens []*domain.Token
	Total  int64
}
