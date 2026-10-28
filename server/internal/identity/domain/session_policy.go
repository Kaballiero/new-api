package domain

import "time"

const (
	RefreshCookieName      = "new_api_refresh"
	SessionHintCookieName  = "new_api_has_session"
	SessionHintCookieValue = "1"
	AccessTokenTTL         = 15 * time.Minute
	SecurityProofTTL       = time.Minute
	LoginSessionTTL        = 30 * 24 * time.Hour
	RefreshReplayWindow    = 30 * time.Second
	LoginVerificationTTL   = 5 * time.Minute
)
