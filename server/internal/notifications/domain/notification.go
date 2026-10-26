package domain

import "errors"

var ErrLimitExceeded = errors.New("notification limit exceeded")

const (
	Email             = "email"
	Webhook           = "webhook"
	Bark              = "bark"
	Gotify            = "gotify"
	ChannelUpdate     = "channel_update"
	ContentValueParam = "{{value}}"
)

type Notice struct {
	Type, Title, Content string
	Values               []any
}
type Preferences struct {
	NotifyType, NotificationEmail, WebhookUrl, WebhookSecret, BarkUrl, GotifyUrl, GotifyToken string
	GotifyPriority                                                                            int
	UpstreamModelUpdateNotifyEnabled                                                          bool
}

// Recipient defers settings decoding until its turn in sequential delivery.
type Recipient struct {
	ID          int
	Email       string
	Preferences func() Preferences
}
