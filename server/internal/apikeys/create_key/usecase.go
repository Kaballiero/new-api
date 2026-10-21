package create_key

import "github.com/QuantumNous/new-api/internal/apikeys/domain"

type Store interface {
	Count(int) (int64, error)
	Insert(*domain.Token) error
	SetAutoGroups(*domain.Token, []string) error
}

func (usecase Usecase) Execute(input Input) (*domain.Token, error) {
	token := input.Token
	if len(token.Name) > 50 {
		return nil, domain.ErrNameTooLong
	}
	params := usecase.audit()
	params["name"] = token.Name
	// A limited key must have a quota within the valid range.
	if !token.UnlimitedQuota {
		if token.RemainQuota < 0 {
			return nil, domain.ErrQuotaNegative
		}
		maxQuotaValue := usecase.maxQuota()
		if token.RemainQuota > maxQuotaValue {
			return nil, &domain.LimitError{Err: domain.ErrQuotaExceedMax, Max: maxQuotaValue}
		}
	}
	// The user must stay below the key count limit.
	maxTokens := usecase.tokenLimit()
	count, err := usecase.store.Count(input.UserID())
	if err != nil {
		return nil, domain.Internal(err)
	}
	if int(count) >= maxTokens {
		return nil, &domain.LimitError{Err: domain.ErrKeyCountLimit, Max: maxTokens}
	}
	if token.Group == "auto" {
		if err := usecase.groups.Apply(&token, input.AutoGroups); err != nil {
			return nil, err
		}
	} else {
		token.CrossGroupRetry = false
		_ = usecase.store.SetAutoGroups(&token, nil)
	}
	key, err := usecase.generate()
	if err != nil {
		usecase.log("failed to generate token key: " + err.Error())
		return nil, domain.ErrKeyGenerate
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
		return nil, domain.Internal(err)
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
