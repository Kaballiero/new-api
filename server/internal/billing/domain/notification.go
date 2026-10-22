package domain

// Recipient contains only settings used by existing notification delivery.
// Credentials are carried to delivery and never included in billing diagnostics.
type Recipient struct {
	UserID                                                                        int
	Email, Language, NotifyType                                                   string
	NotificationEmail, WebhookURL, WebhookSecret, BarkURL, GotifyURL, GotifyToken string
	GotifyPriority                                                                int
	WarningThreshold                                                              float64
}

type NotificationState struct {
	Recipient                                                       Recipient
	UserQuota                                                       int
	SubscriptionID                                                  int
	SubscriptionTotal, SubscriptionUsedAfter, SubscriptionPostDelta int64
}

type Notice struct {
	Recipient    Recipient
	Subscription bool
	Remaining    int
}

func QuotaNotice(state NotificationState, delta, preConsumed, threshold int, subscription bool) (Notice, bool) {
	if state.Recipient.WarningThreshold != 0 {
		threshold = int(state.Recipient.WarningThreshold)
	}
	notice := Notice{Recipient: state.Recipient, Subscription: subscription, Remaining: state.UserQuota}
	if subscription {
		if state.SubscriptionID == 0 || state.SubscriptionTotal <= 0 {
			return notice, false
		}
		remaining := state.SubscriptionTotal - state.SubscriptionUsedAfter - state.SubscriptionPostDelta
		notice.Remaining = int(remaining)
		return notice, remaining < int64(threshold)
	}
	return notice, state.UserQuota-(delta+preConsumed) < threshold
}
