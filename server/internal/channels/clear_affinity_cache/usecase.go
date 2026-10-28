package clear_affinity_cache

import "errors"

// ErrRuleRequired stays in the scenario: channels/domain still depends on
// common and would make this usecase transitively depend on Gin.
var ErrRuleRequired = errors.New("rule_name or all=true is required")

type Cache interface {
	ClearAll() int
	ClearRule(string) (int, error)
}
type Usecase struct{ cache Cache }

func NewUsecase(cache Cache) Usecase { return Usecase{cache: cache} }
func (usecase Usecase) Execute(input Input) (Output, error) {
	if input.All == "true" {
		return Output{Deleted: usecase.cache.ClearAll()}, nil
	}
	if input.Rule == "" {
		return Output{}, ErrRuleRequired
	}
	count, err := usecase.cache.ClearRule(input.Rule)
	return Output{Deleted: count}, err
}
