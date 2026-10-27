package identityhttp

import (
	identitystore "github.com/QuantumNous/new-api/internal/adapter/gorm/identity"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/identity/session"
	"github.com/QuantumNous/new-api/internal/identity/domain"

	"github.com/gin-gonic/gin"
)

func WriteRefreshCookie(sql identitystore.Store, c *gin.Context, rawToken string) {
	expiresAt := time.Now().Add(domain.LoginSessionTTL)
	if sid, _, ok := session.SplitRefreshToken(rawToken); ok {
		if session, err := sql.GetUserSessionCached(sid); err == nil && session.ExpiresAt > time.Now().Unix() {
			expiresAt = time.Unix(session.ExpiresAt, 0)
		}
	}
	maxAge := max(int(time.Until(expiresAt)/time.Second), 1)
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     domain.RefreshCookieName,
		Value:    rawToken,
		Path:     "/api/user/auth",
		MaxAge:   maxAge,
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   common.SessionCookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
	writeSessionHintCookie(c, maxAge, expiresAt)
}

func ClearRefreshCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     domain.RefreshCookieName,
		Value:    "",
		Path:     "/api/user/auth",
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
		HttpOnly: true,
		Secure:   common.SessionCookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
	clearSessionHintCookie(c)
}

// writeSessionHintCookie mirrors the Refresh Cookie's lifetime with a
// script-readable marker. The Refresh Cookie itself is HttpOnly and scoped to
// /api/user/auth, so a page at / cannot tell whether a login session exists;
// without this hint the frontend has to POST /api/user/auth/refresh on every
// cold boot just to learn that an anonymous visitor is anonymous. That request
// is guaranteed to 401 and still consumes a slot of the IP-keyed
// CriticalRateLimit budget shared by everyone behind the same address.
//
// The value is the constant "1" and carries no credential: it states that a
// Refresh Cookie was issued, never who for. Authorization still derives solely
// from the Refresh Cookie and the Access Token, so forging this hint only costs
// the forger the round trip it was meant to avoid.
//
// It must be written and cleared in lockstep with the Refresh Cookie, which is
// why it lives inside these two helpers rather than at their call sites: both
// cookies then ride the same response with the same expiry, and no login path
// can set one without the other.

func writeSessionHintCookie(c *gin.Context, maxAge int, expiresAt time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     domain.SessionHintCookieName,
		Value:    domain.SessionHintCookieValue,
		Path:     "/",
		MaxAge:   maxAge,
		Expires:  expiresAt,
		HttpOnly: false,
		Secure:   common.SessionCookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
}

func clearSessionHintCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     domain.SessionHintCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
		HttpOnly: false,
		Secure:   common.SessionCookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
}
