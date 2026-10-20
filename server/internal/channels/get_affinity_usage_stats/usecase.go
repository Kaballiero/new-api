package get_affinity_usage_stats

import "errors"

type Usecase struct {
	read func(string, string, string) Output
}

func NewUsecase(read func(string, string, string) Output) Usecase { return Usecase{read: read} }
func (usecase Usecase) Execute(input Input) (Output, error) {
	if input.Rule == "" {
		return Output{}, errors.New("missing param: rule_name")
	}
	if input.Key == "" {
		return Output{}, errors.New("missing param: key_fp")
	}
	return usecase.read(input.Rule, input.Group, input.Key), nil
}
