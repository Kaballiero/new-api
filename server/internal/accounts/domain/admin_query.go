package domain

import "time"

// User содержит административную проекцию аккаунта без деталей хранения.
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

// UserBatchRow содержит данные пакетного чтения и роль для проверки доступа.
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
