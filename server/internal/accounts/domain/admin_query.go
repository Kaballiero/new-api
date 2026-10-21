package domain

import "time"

// User is the administrative projection of an account without storage details.
type User struct {
	Id               int
	Username         string
	Password         string
	OriginalPassword string
	DisplayName      string
	Role             int
	Status           int
	Email            string
	GitHubId         string
	DiscordId        string
	OidcId           string
	WeChatId         string
	TelegramId       string
	VerificationCode string
	Quota            int
	UsedQuota        int
	RequestCount     int
	Group            string
	AffCode          string
	AffCount         int
	AffQuota         int
	AffHistoryQuota  int
	InviterId        int
	DeletedAt        *time.Time
	LinuxDOId        string
	Setting          string
	Remark           string
	StripeCustomer   string
	CreatedAt        int64
	LastLoginAt      int64
	AdminPermissions map[string]map[string]bool
}

// UserBatchRow holds the batch read data and the role for the access check.
type UserBatchRow struct {
	Id           int
	Role         int
	Status       int
	Quota        int
	UsedQuota    int
	RequestCount int
	Group        string
	DeletedAt    *time.Time
}
