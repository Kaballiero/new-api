package search_keys

import (
	"strings"

	"github.com/QuantumNous/new-api/internal/apikeys/domain"
)

type Query interface {
	NamePattern(string)
	KeyPattern(string)
	Count(limit int) (int64, error)
	List(offset, limit int) ([]*domain.Token, error)
}
type Reader interface {
	Count(int) (int64, error)
	OpenSearch(userID int) Query
}
type Usecase struct {
	reader        Reader
	maxTokens     func() int
	log, errorLog func(string)
}

func NewUsecase(reader Reader, maxTokens func() int, log, errorLog func(string)) Usecase {
	return Usecase{reader: reader, maxTokens: maxTokens, log: log, errorLog: errorLog}
}
func (usecase Usecase) Execute(input Input) (Output, error) {
	limit, offset := input.Limit, input.Start
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	keyword, key := input.Keyword, input.Key
	if key != "" {
		key = strings.TrimPrefix(key, "sk-")
	}
	maxTokens := usecase.maxTokens()
	hasFuzzy := strings.Contains(keyword, "%") || strings.Contains(key, "%")
	if hasFuzzy {
		count, err := usecase.reader.Count(input.UserID)
		if err != nil {
			usecase.log("failed to count user tokens: " + err.Error())
			return Output{}, domain.ErrSearchCountFailed
		}
		if int(count) > maxTokens {
			return Output{}, domain.ErrFuzzySearchLimit
		}
	}
	query := usecase.reader.OpenSearch(input.UserID)
	if keyword != "" {
		pattern, err := domain.SanitizeSearchPattern(keyword)
		if err != nil {
			return Output{}, err
		}
		query.NamePattern(pattern)
	}
	if key != "" {
		pattern, err := domain.SanitizeSearchPattern(key)
		if err != nil {
			return Output{}, err
		}
		query.KeyPattern(pattern)
	}
	total, err := query.Count(maxTokens)
	if err != nil {
		usecase.errorLog("failed to count search tokens: " + err.Error())
		return Output{}, domain.ErrSearchFailed
	}
	tokens, err := query.List(offset, limit)
	if err != nil {
		usecase.errorLog("failed to search tokens: " + err.Error())
		return Output{}, domain.ErrSearchFailed
	}
	return Output{Tokens: tokens, Total: total}, nil
}
