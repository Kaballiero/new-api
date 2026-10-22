package validate_key

import (
	"context"
	"fmt"
	"net"

	"github.com/QuantumNous/new-api/internal/apikeys/domain"
)

type Store interface {
	Lookup(string) (*domain.Token, error)
	UpdateStatus(*domain.Token) error
	IsMissing(error) bool
	Owner(int) (*domain.Owner, error)
	IsAdmin(int) bool
	AutoGroups(*domain.Token) ([]string, bool)
}

type Rules struct {
	Now          func() int64
	CacheEnabled func() bool
	UsableGroups func(string) map[string]string
	GroupExists  func(string) bool
	IPAllowed    func(net.IP, []string) bool
	Log          func(string)
	Debug        func(context.Context, string, ...any)
}

type Usecase struct {
	store Store
	rules Rules
}

func NewUsecase(store Store, rules Rules) Usecase { return Usecase{store: store, rules: rules} }

type Input struct {
	Key      string
	ReadOnly bool
	ClientIP string
	Channel  *string
}

// Execute authenticates one gateway key, retaining the legacy order of checks.
func (u Usecase) Execute(ctx context.Context, input Input) (domain.Caller, error) {
	caller := domain.Caller{}
	if !input.ReadOnly && input.Key == "" {
		return caller, domain.ErrKeyNotProvided
	}
	token, err := u.store.Lookup(input.Key)
	caller.Token = token
	if err != nil {
		if !input.ReadOnly {
			u.rules.Log("ValidateUserToken: failed to get token: " + err.Error())
		}
		if u.store.IsMissing(err) {
			return caller, domain.ErrKeyInvalid
		}
		if input.ReadOnly {
			u.rules.Log("TokenAuthReadOnly GetTokenByKey database error: " + err.Error())
		} else {
			u.rules.Log("TokenAuth ValidateUserToken database error: " + err.Error())
		}
		return caller, domain.Internal(err)
	}
	if input.ReadOnly {
		// Expired and exhausted keys can read their usage; disabled keys cannot.
		if token.Status == domain.StatusDisabled {
			return caller, domain.ErrKeyDisabled
		}
	} else {
		if token.Status != domain.StatusEnabled {
			return caller, domain.ErrKeyInvalid
		}
		status := token.Status
		if token.ExpiredTime != -1 && token.ExpiredTime < u.rules.Now() {
			status = domain.StatusExpired
		} else if !token.UnlimitedQuota && token.RemainQuota <= 0 {
			status = domain.StatusExhausted
		}
		if status != token.Status {
			if !u.rules.CacheEnabled() {
				token.Status = status
				if err := u.store.UpdateStatus(token); err != nil {
					u.rules.Log("failed to update token status" + err.Error())
				}
			}
			return caller, domain.ErrKeyInvalid
		}
		if ips := token.GetIpLimits(); len(ips) > 0 {
			u.rules.Debug(ctx, "Token has IP restrictions, checking client IP %s", input.ClientIP)
			ip := net.ParseIP(input.ClientIP)
			if ip == nil {
				return caller, domain.ErrIPInvalid
			}
			if !u.rules.IPAllowed(ip, ips) {
				return caller, domain.ErrIPDenied
			}
			u.rules.Debug(ctx, "Client IP %s passed the token IP restrictions check", input.ClientIP)
		}
	}
	owner, err := u.store.Owner(token.UserId)
	if err != nil {
		mode := "TokenAuth"
		if input.ReadOnly {
			mode = "TokenAuthReadOnly"
		}
		u.rules.Log(fmt.Sprintf("%s GetUserCache error for user %d: %v", mode, token.UserId, err))
		return caller, domain.Internal(err)
	}
	if owner.Status != domain.OwnerStatusEnabled {
		return caller, domain.ErrOwnerBanned
	}
	if input.ReadOnly {
		return caller, nil
	}
	caller.Owner = owner
	group := owner.Group
	if token.Group != "" {
		if _, ok := u.rules.UsableGroups(group)[token.Group]; !ok {
			return caller, &domain.GroupError{Err: domain.ErrGroupForbidden, Group: token.Group}
		}
		if !u.rules.GroupExists(token.Group) && token.Group != "auto" {
			return caller, &domain.GroupError{Err: domain.ErrGroupDeprecated, Group: token.Group}
		}
		group = token.Group
	}
	caller.UsingGroup = &group
	caller.Context, err = u.PrepareContext(ContextInput{Token: token, Channel: input.Channel})
	return caller, err
}

type ContextInput struct {
	Token   *domain.Token
	Channel *string
}

// PrepareContext supports the existing playground consumer without authenticating
// its synthetic key against storage. All token context rules have one owner.
func (u Usecase) PrepareContext(input ContextInput) (*domain.KeyContext, error) {
	if input.Token == nil {
		return nil, fmt.Errorf("token is nil")
	}
	groups, present := u.store.AutoGroups(input.Token)
	admin := false
	if input.Channel != nil {
		admin = u.store.IsAdmin(input.Token.UserId)
	}
	return domain.ContextForKey(input.Token, groups, present, input.Channel, admin)
}
