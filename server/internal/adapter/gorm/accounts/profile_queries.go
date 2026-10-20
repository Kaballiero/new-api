package accountsadapter

import (
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/QuantumNous/new-api/internal/accounts/get_affiliate_code"
	"github.com/QuantumNous/new-api/model"
)

// ProfileQueriesStore адаптирует существующие чтение профиля и изменение кода приглашения.
type ProfileQueriesStore struct{}

func (ProfileQueriesStore) SelfProfile(id int) (domain.SelfProfile, error) {
	user, err := model.GetSelfUserById(id)
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

func (ProfileQueriesStore) AffiliateAccount(id int) (get_affiliate_code.AffiliateAccount, error) {
	user, err := model.GetUserById(id, true)
	if err != nil {
		return nil, err
	}
	return affiliateAccount{user: user}, nil
}

type affiliateAccount struct{ user *model.User }

func (account affiliateAccount) AffiliateCode() string { return account.user.AffCode }
func (account affiliateAccount) SaveAffiliateCode(code string) error {
	account.user.AffCode = code
	return account.user.Update(false)
}
