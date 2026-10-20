package search_keys

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/apikeys/domain"
	"strings"
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
func (usecase Usecase) Execute(input Input) (Output, *domain.Failure) {
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
			return Output{}, &domain.Failure{Kind: "internal", Cause: errors.New("获取令牌数量失败")}
		}
		if int(count) > maxTokens {
			return Output{}, &domain.Failure{Kind: "internal", Cause: errors.New("令牌数量超过上限，仅允许精确搜索，请勿使用 % 通配符")}
		}
	}
	query := usecase.reader.OpenSearch(input.UserID)
	if keyword != "" {
		pattern, err := domain.SanitizeSearchPattern(keyword)
		if err != nil {
			return Output{}, &domain.Failure{Kind: "internal", Cause: err}
		}
		query.NamePattern(pattern)
	}
	if key != "" {
		pattern, err := domain.SanitizeSearchPattern(key)
		if err != nil {
			return Output{}, &domain.Failure{Kind: "internal", Cause: err}
		}
		query.KeyPattern(pattern)
	}
	total, err := query.Count(maxTokens)
	if err != nil {
		usecase.errorLog("failed to count search tokens: " + err.Error())
		return Output{}, &domain.Failure{Kind: "internal", Cause: errors.New("搜索令牌失败")}
	}
	tokens, err := query.List(offset, limit)
	if err != nil {
		usecase.errorLog("failed to search tokens: " + err.Error())
		return Output{}, &domain.Failure{Kind: "internal", Cause: errors.New("搜索令牌失败")}
	}
	return Output{Tokens: tokens, Total: total}, nil
}
