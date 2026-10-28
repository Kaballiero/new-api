package get_affinity_usage_stats

import "errors"

// The errors stay in the scenario: channels/domain still depends on common
// and would make this usecase transitively depend on Gin.
var (
	ErrRuleNameMissing = errors.New("missing param: rule_name")
	ErrKeyMissing      = errors.New("missing param: key_fp")
)

type Usecase struct {
	read func(string, string, string) Output
}

func NewUsecase(read func(string, string, string) Output) Usecase { return Usecase{read: read} }
func (usecase Usecase) Execute(input Input) (Output, error) {
	if input.Rule == "" {
		return Output{}, ErrRuleNameMissing
	}
	if input.Key == "" {
		return Output{}, ErrKeyMissing
	}
	return usecase.read(input.Rule, input.Group, input.Key), nil
}
