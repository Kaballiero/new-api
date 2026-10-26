package notifications

import (
	"github.com/QuantumNous/new-api/internal/notifications/domain"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

func Preferences(s dto.UserSetting) domain.Preferences {
	return domain.Preferences{NotifyType: s.NotifyType, NotificationEmail: s.NotificationEmail, WebhookUrl: s.WebhookUrl, WebhookSecret: s.WebhookSecret, BarkUrl: s.BarkUrl, GotifyUrl: s.GotifyUrl, GotifyToken: s.GotifyToken, GotifyPriority: s.GotifyPriority, UpstreamModelUpdateNotifyEnabled: s.UpstreamModelUpdateNotifyEnabled}
}
func Notice(n dto.Notify) domain.Notice {
	return domain.Notice{Type: n.Type, Title: n.Title, Content: n.Content, Values: n.Values}
}
