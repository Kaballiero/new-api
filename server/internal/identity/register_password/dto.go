package register_password

import "time"

type User struct {
	Id                   int                        `json:"id"`
	Username             string                     `json:"username" validate:"max=20"`
	Password             string                     `json:"password" validate:"min=8,max=128"`
	HasPassword          bool                       `json:"-"`
	OriginalPassword     string                     `json:"original_password"` // this field is only for Password change verification, don't save it to database!
	DisplayName          string                     `json:"display_name" validate:"max=20"`
	Role                 int                        `json:"role"`   // admin, common
	Status               int                        `json:"status"` // enabled, disabled
	Email                string                     `json:"email" validate:"max=50"`
	GitHubId             string                     `json:"github_id"`
	DiscordId            string                     `json:"discord_id"`
	OidcId               string                     `json:"oidc_id"`
	WeChatId             string                     `json:"wechat_id"`
	TelegramId           string                     `json:"telegram_id"`
	VerificationCode     string                     `json:"verification_code"` // this field is only for Email verification, don't save it to database!
	AccessToken          *string                    `json:"-"`                 // this token is for system management
	AccessTokenCreatedAt *int64                     `json:"-"`
	Quota                int                        `json:"quota"`
	BatchHeldQuota       int64                      `json:"-"`
	BatchQuotaVersion    int64                      `json:"-"`
	UsedQuota            int                        `json:"used_quota"`    // used quota
	RequestCount         int                        `json:"request_count"` // request number
	Group                string                     `json:"group"`
	AffCode              string                     `json:"aff_code"`
	AffCount             int                        `json:"aff_count"`
	AffQuota             int                        `json:"aff_quota"`         // Remaining invitation quota
	AffHistoryQuota      int                        `json:"aff_history_quota"` // Total invitation quota
	InviterId            int                        `json:"inviter_id"`
	DeletedAt            *time.Time                 ``
	LinuxDOId            string                     `json:"linux_do_id"`
	Setting              string                     `json:"setting"`
	Remark               string                     `json:"remark,omitempty" validate:"max=255"`
	StripeCustomer       string                     `json:"stripe_customer"`
	CreatedAt            int64                      `json:"created_at"`
	LastLoginAt          int64                      `json:"last_login_at"`
	AuthVersion          int64                      `json:"-"`
	AdminPermissions     map[string]map[string]bool `json:"admin_permissions,omitempty"`
}

// Input is the registration request and the request language of the persisted
// registration texts.
type Input struct {
	User User
	Lang string
}
