package update_key

import "github.com/QuantumNous/new-api/internal/apikeys/domain"

type Store interface {
	Get(int, int) (*domain.Token, error)
	IsMissing(error) bool
	Update(*domain.Token) error
	SetAutoGroups(*domain.Token, []string) error
}

func (usecase Usecase) Execute(input Input) (*domain.Token, error) {
	token := input.Token
	params := usecase.audit()
	if token.Id > 0 {
		params["id"] = token.Id
	}
	if len(token.Name) > 50 {
		return nil, domain.ErrNameTooLong
	}
	if !token.UnlimitedQuota {
		if token.RemainQuota < 0 {
			return nil, domain.ErrQuotaNegative
		}
		maxQuotaValue := usecase.maxQuota()
		if token.RemainQuota > maxQuotaValue {
			return nil, &domain.LimitError{Err: domain.ErrQuotaExceedMax, Max: maxQuotaValue}
		}
	}
	cleanToken, err := usecase.store.Get(token.Id, input.UserID)
	if usecase.store.IsMissing(err) {
		return nil, domain.ErrKeyNotFound
	}
	if err != nil {
		return nil, domain.Internal(err)
	}
	params["name"] = cleanToken.Name
	previous := *cleanToken
	if token.Status == domain.StatusEnabled {
		if cleanToken.Status == domain.StatusExpired && cleanToken.ExpiredTime <= usecase.now() && cleanToken.ExpiredTime != -1 {
			return nil, domain.ErrExpiredCannotEnable
		}
		if cleanToken.Status == domain.StatusExhausted && cleanToken.RemainQuota <= 0 && !cleanToken.UnlimitedQuota {
			return nil, domain.ErrExhaustedCannotEnable
		}
	}
	if input.StatusOnly != "" {
		cleanToken.Status = token.Status
	} else {
		// If you add more fields, please also update token.Update()
		cleanToken.Name = token.Name
		cleanToken.ExpiredTime = token.ExpiredTime
		cleanToken.RemainQuota = token.RemainQuota
		cleanToken.UnlimitedQuota = token.UnlimitedQuota
		cleanToken.ModelLimitsEnabled = token.ModelLimitsEnabled
		cleanToken.ModelLimits = token.ModelLimits
		cleanToken.AllowIps = token.AllowIps
		cleanToken.Group = token.Group
		cleanToken.CrossGroupRetry = token.CrossGroupRetry
		if token.Group != "auto" {
			cleanToken.CrossGroupRetry = false
			_ = usecase.store.SetAutoGroups(cleanToken, nil)
		} else if input.AutoGroupsSet {
			if err := usecase.groups.Apply(cleanToken, input.AutoGroups); err != nil {
				return nil, err
			}
		}
	}
	if err := usecase.store.Update(cleanToken); err != nil {
		return nil, domain.Internal(err)
	}
	params["name"] = cleanToken.Name
	if input.StatusOnly != "" {
		params["from"], params["to"] = previous.Status, cleanToken.Status
	} else {
		changedFields := []string{}
		for _, field := range []struct {
			name    string
			changed bool
		}{
			{"name", previous.Name != cleanToken.Name},
			{"expired_time", previous.ExpiredTime != cleanToken.ExpiredTime},
			{"remain_quota", previous.RemainQuota != cleanToken.RemainQuota},
			{"unlimited_quota", previous.UnlimitedQuota != cleanToken.UnlimitedQuota},
			{"model_limits_enabled", previous.ModelLimitsEnabled != cleanToken.ModelLimitsEnabled},
			{"model_limits", previous.ModelLimits != cleanToken.ModelLimits},
			{"allow_ips", (previous.AllowIps == nil) != (cleanToken.AllowIps == nil) ||
				(previous.AllowIps != nil && cleanToken.AllowIps != nil && *previous.AllowIps != *cleanToken.AllowIps)},
			{"group", previous.Group != cleanToken.Group},
			{"cross_group_retry", previous.CrossGroupRetry != cleanToken.CrossGroupRetry},
			{"auto_groups", previous.AutoGroups != cleanToken.AutoGroups},
		} {
			if field.changed {
				changedFields = append(changedFields, field.name)
			}
		}
		params["changed_fields"] = changedFields
	}
	usecase.succeed()

	return cleanToken, nil
}

type Usecase struct {
	groups   domain.AutoGroupRules
	store    Store
	maxQuota func() int
	now      func() int64
	audit    func() map[string]any
	succeed  func()
}

func NewUsecase(store Store, groups domain.AutoGroupRules, maxQuota func() int, now func() int64, audit func() map[string]any, succeed func()) Usecase {
	return Usecase{store: store, groups: groups, maxQuota: maxQuota, now: now, audit: audit, succeed: succeed}
}
