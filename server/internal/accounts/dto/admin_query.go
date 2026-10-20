package dto

import (
	"time"

	"github.com/QuantumNous/new-api/internal/accounts/domain"
)

// User сохраняет поля административного ответа аккаунта.
type User struct {
	Id               int    `json:"id"`
	Username         string `json:"username" validate:"max=20"`
	Password         string `json:"password" validate:"min=8,max=128"`
	OriginalPassword string `json:"original_password"`
	DisplayName      string `json:"display_name" validate:"max=20"`
	Role             int    `json:"role"`
	Status           int    `json:"status"`
	Email            string `json:"email" validate:"max=50"`
	GitHubId         string `json:"github_id"`
	DiscordId        string `json:"discord_id"`
	OidcId           string `json:"oidc_id"`
	WeChatId         string `json:"wechat_id"`
	TelegramId       string `json:"telegram_id"`
	VerificationCode string `json:"verification_code"`
	Quota            int    `json:"quota"`
	UsedQuota        int    `json:"used_quota"`
	RequestCount     int    `json:"request_count"`
	Group            string `json:"group"`
	AffCode          string `json:"aff_code"`
	AffCount         int    `json:"aff_count"`
	AffQuota         int    `json:"aff_quota"`
	AffHistoryQuota  int    `json:"aff_history_quota"`
	InviterId        int    `json:"inviter_id"`
	DeletedAt        *time.Time
	LinuxDOId        string                     `json:"linux_do_id"`
	Setting          string                     `json:"setting"`
	Remark           string                     `json:"remark,omitempty" validate:"max=255"`
	StripeCustomer   string                     `json:"stripe_customer"`
	CreatedAt        int64                      `json:"created_at"`
	LastLoginAt      int64                      `json:"last_login_at"`
	AdminPermissions map[string]map[string]bool `json:"admin_permissions,omitempty"`
}

func (user User) Domain() *domain.User {
	value := domain.User(user)
	return &value
}

func UserFromDomain(user *domain.User) *User {
	if user == nil {
		return nil
	}
	value := User(*user)
	return &value
}

func UsersFromDomain(users []*domain.User) []*User {
	if users == nil {
		return nil
	}
	result := make([]*User, len(users))
	for i, user := range users {
		result[i] = UserFromDomain(user)
	}
	return result
}
