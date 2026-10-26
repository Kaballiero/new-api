package domain

// UserSetting holds account preferences independently of their wire representation.
type UserSetting struct {
	NotifyType                       string
	QuotaWarningThreshold            float64
	WebhookUrl                       string
	WebhookSecret                    string
	NotificationEmail                string
	BarkUrl                          string
	GotifyUrl                        string
	GotifyToken                      string
	GotifyPriority                   int
	UpstreamModelUpdateNotifyEnabled bool
	AcceptUnsetRatioModel            bool
	RecordIpLog                      bool
	SidebarModules                   string
	BillingPreference                string
	Language                         string
}
