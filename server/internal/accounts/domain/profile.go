package domain

// SelfProfile holds only the publishable data of the own profile.
type SelfProfile struct {
	Id              int
	Username        string
	DisplayName     string
	HasPassword     bool
	Role            int
	Status          int
	Email           string
	GitHubId        string
	DiscordId       string
	OidcId          string
	WeChatId        string
	TelegramId      string
	Group           string
	Quota           int
	UsedQuota       int
	RequestCount    int
	AffCode         string
	AffCount        int
	AffQuota        int
	AffHistoryQuota int
	InviterId       int
	LinuxDOId       string
	Setting         string
	StripeCustomer  string
}
