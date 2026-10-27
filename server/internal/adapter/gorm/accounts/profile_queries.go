package accountsadapter

import (
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/QuantumNous/new-api/internal/accounts/get_affiliate_code"
	core "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/core"
	"github.com/QuantumNous/new-api/model"
)

// ProfileQueriesStore адаптирует существующие чтение профиля и изменение кода приглашения.
type ProfileQueriesStore struct {
	SQL    core.Store
	Update func(*model.User, bool) error
}

func (store ProfileQueriesStore) SelfProfile(id int) (domain.SelfProfile, error) {
	user, err := store.SQL.GetSelfUserById(id)
	if err != nil {
		return domain.SelfProfile{}, err
	}
	return ProfileData(user), nil
}

// ProfileData исключает реквизиты аутентификации из нейтральной проекции.
func ProfileData(user *model.User) domain.SelfProfile {
	return domain.SelfProfile{
		Id:              user.Id,
		Username:        user.Username,
		DisplayName:     user.DisplayName,
		HasPassword:     user.HasPassword,
		Role:            user.Role,
		Status:          user.Status,
		Email:           user.Email,
		GitHubId:        user.GitHubId,
		DiscordId:       user.DiscordId,
		OidcId:          user.OidcId,
		WeChatId:        user.WeChatId,
		TelegramId:      user.TelegramId,
		Group:           user.Group,
		Quota:           user.Quota,
		UsedQuota:       user.UsedQuota,
		RequestCount:    user.RequestCount,
		AffCode:         user.AffCode,
		AffCount:        user.AffCount,
		AffQuota:        user.AffQuota,
		AffHistoryQuota: user.AffHistoryQuota,
		InviterId:       user.InviterId,
		LinuxDOId:       user.LinuxDOId,
		Setting:         user.Setting,
		StripeCustomer:  user.StripeCustomer,
	}
}

func (store ProfileQueriesStore) AffiliateAccount(id int) (get_affiliate_code.AffiliateAccount, error) {
	user, err := store.SQL.GetUserById(id, true)
	if err != nil {
		return nil, err
	}
	return affiliateAccount{user: user, update: store.Update}, nil
}

type affiliateAccount struct {
	user   *model.User
	update func(*model.User, bool) error
}

func (account affiliateAccount) AffiliateCode() string { return account.user.AffCode }
func (account affiliateAccount) SaveAffiliateCode(code string) error {
	account.user.AffCode = code
	return account.update(account.user, false)
}
