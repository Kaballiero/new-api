package clear_affinity_cache

import "errors"

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
		return Output{}, errors.New("缺少参数：rule_name，或使用 all=true 清空全部")
	}
	count, err := usecase.cache.ClearRule(input.Rule)
	return Output{Deleted: count}, err
}
