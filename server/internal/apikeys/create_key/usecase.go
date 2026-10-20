package create_key

import "github.com/QuantumNous/new-api/internal/apikeys/domain"

type Store interface {
	Count(int) (int64, error)
	Insert(*domain.Token) error
	SetAutoGroups(*domain.Token, []string) error
}

func (usecase Usecase) Execute(input Input) (*domain.Token, *domain.Failure) {
	token := input.Token
	if len(token.Name) > 50 {
		return nil, &domain.Failure{Kind: "name"}
	}
	params := usecase.audit()
	params["name"] = token.Name
	// 非无限额度时，检查额度值是否超出有效范围
	if !token.UnlimitedQuota {
		if token.RemainQuota < 0 {
			return nil, &domain.Failure{Kind: "quota_negative"}
		}
		maxQuotaValue := usecase.maxQuota()
		if token.RemainQuota > maxQuotaValue {
			return nil, &domain.Failure{Kind: "quota_exceed", Params: map[string]any{"Max": maxQuotaValue}}
		}
	}
	// 检查用户令牌数量是否已达上限
	maxTokens := usecase.tokenLimit()
	count, err := usecase.store.Count(input.UserID())
	if err != nil {
		return nil, &domain.Failure{Kind: "internal", Cause: err}
	}
	if int(count) >= maxTokens {
		return nil, &domain.Failure{Kind: "count", Params: map[string]any{"Max": maxTokens}}
	}
	if token.Group == "auto" {
		if failure := usecase.groups.Apply(&token, input.AutoGroups); failure != nil {
			return nil, failure
		}
	} else {
		token.CrossGroupRetry = false
		_ = usecase.store.SetAutoGroups(&token, nil)
	}
	key, err := usecase.generate()
	if err != nil {
		usecase.log("failed to generate token key: " + err.Error())
		return nil, &domain.Failure{Kind: "generate"}
	}
	cleanToken := domain.Token{
		UserId:             input.UserID(),
		Name:               token.Name,
		Key:                key,
		CreatedTime:        usecase.now(),
		AccessedTime:       usecase.now(),
		ExpiredTime:        token.ExpiredTime,
		RemainQuota:        token.RemainQuota,
		UnlimitedQuota:     token.UnlimitedQuota,
		ModelLimitsEnabled: token.ModelLimitsEnabled,
		ModelLimits:        token.ModelLimits,
		AllowIps:           token.AllowIps,
		Group:              token.Group,
		CrossGroupRetry:    token.CrossGroupRetry,
		AutoGroups:         token.AutoGroups,
	}
	if err := usecase.store.Insert(&cleanToken); err != nil {
		return nil, &domain.Failure{Kind: "internal", Cause: err}
	}
	params["id"] = cleanToken.Id
	usecase.succeed()

	return &cleanToken, nil
}

type Usecase struct {
	groups     domain.AutoGroupRules
	store      Store
	maxQuota   func() int
	tokenLimit func() int
	now        func() int64
	generate   func() (string, error)
	log        func(string)
	audit      func() map[string]any
	succeed    func()
}

func NewUsecase(store Store, groups domain.AutoGroupRules, maxQuota func() int, tokenLimit func() int, now func() int64, generate func() (string, error), log func(string), audit func() map[string]any, succeed func()) Usecase {
	return Usecase{store: store, groups: groups, maxQuota: maxQuota, tokenLimit: tokenLimit, now: now, generate: generate, log: log, audit: audit, succeed: succeed}
}
