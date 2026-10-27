package authtoken

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	identitystore "github.com/QuantumNous/new-api/internal/adapter/gorm/identity"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/identity/domain"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	accessTokenUse        = "access"
	securityProofTokenUse = "security_proof"
	authTokenIssuer       = "new-api"
	authTokenAudience     = "new-api-dashboard"
)

type authClaims struct {
	TokenUse        string   `json:"token_use"`
	SessionID       string   `json:"sid"`
	UserAuthVersion int64    `json:"uv"`
	SessionVersion  int64    `json:"sv"`
	Method          string   `json:"method,omitempty"`
	Scopes          []string `json:"scopes,omitempty"`
	ContextHash     string   `json:"context_hash,omitempty"`
	jwt.RegisteredClaims
}

func SigningKey(purpose string) []byte {
	mac := hmac.New(sha256.New, []byte(common.SessionSecret))
	_, _ = mac.Write([]byte("new-api/auth/" + purpose + "/v1"))
	return mac.Sum(nil)
}

func IssueAccessToken(identity domain.AuthIdentity) (string, int64, error) {
	if identity.UserID <= 0 || identity.SessionID == "" || identity.UserAuthVersion <= 0 || identity.SessionVersion <= 0 {
		return "", 0, domain.ErrAuthTokenInvalid
	}
	now := time.Now()
	expiresAt := now.Add(domain.AccessTokenTTL)
	claims := authClaims{
		TokenUse:        accessTokenUse,
		SessionID:       identity.SessionID,
		UserAuthVersion: identity.UserAuthVersion,
		SessionVersion:  identity.SessionVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    authTokenIssuer,
			Subject:   strconv.Itoa(identity.UserID),
			Audience:  jwt.ClaimStrings{authTokenAudience},
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			NotBefore: jwt.NewNumericDate(now.Add(-5 * time.Second)),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        uuid.NewString(),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(SigningKey(accessTokenUse))
	return signed, expiresAt.Unix(), err
}

func ParseAccessToken(raw string) (domain.AuthIdentity, error) {
	claims, err := parseAuthClaims(raw, accessTokenUse, SigningKey(accessTokenUse))
	if err != nil {
		return domain.AuthIdentity{}, err
	}
	userID, err := strconv.Atoi(claims.Subject)
	if err != nil || userID <= 0 || claims.SessionID == "" || claims.UserAuthVersion <= 0 || claims.SessionVersion <= 0 {
		return domain.AuthIdentity{}, domain.ErrAuthTokenInvalid
	}
	return domain.AuthIdentity{
		UserID:          userID,
		SessionID:       claims.SessionID,
		UserAuthVersion: claims.UserAuthVersion,
		SessionVersion:  claims.SessionVersion,
	}, nil
}

// ParseDashboardAccessToken distinguishes new-api dashboard JWTs from opaque
// credentials. A token carrying the dashboard issuer, audience and a known
// token use is always treated as internal, even when its signature, lifetime
// or requested purpose is invalid, so it can never fall through to PAT or
// relay-token authentication.
func ParseDashboardAccessToken(raw string) (identity domain.AuthIdentity, internal bool, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return domain.AuthIdentity{}, false, nil
	}
	claims := &authClaims{}
	parsed, _, parseErr := jwt.NewParser().ParseUnverified(raw, claims)
	if parseErr != nil || parsed == nil {
		return domain.AuthIdentity{}, false, nil
	}
	audienceMatches := slices.Contains(claims.Audience, authTokenAudience)
	knownTokenUse := claims.TokenUse == accessTokenUse || claims.TokenUse == securityProofTokenUse
	if claims.Issuer != authTokenIssuer || !audienceMatches || !knownTokenUse {
		return domain.AuthIdentity{}, false, nil
	}
	identity, err = ParseAccessToken(raw)
	return identity, true, err
}

func IssueSecurityProof(sql identitystore.Store, identity domain.AuthIdentity, method string, binding domain.VerificationBinding) (string, int64, error) {
	method = strings.TrimSpace(method)
	if identity.UserID <= 0 || identity.SessionID == "" || identity.UserAuthVersion <= 0 || identity.SessionVersion <= 0 || method == "" || binding.Scope == "" || binding.ContextHash == "" {
		return "", 0, domain.ErrAuthTokenInvalid
	}
	now := time.Now()
	expiresAt := now.Add(domain.SecurityProofTTL).Truncate(time.Second)
	proofID, _, err := sql.CreateAuthFlow(identitystore.AuthFlowCreate{
		Purpose: domain.AuthFlowPurposeSecurityProof, UserId: identity.UserID,
		SessionId: identity.SessionID, ExpiresAt: expiresAt,
	})
	if err != nil {
		return "", 0, err
	}
	claims := authClaims{
		TokenUse:        securityProofTokenUse,
		SessionID:       identity.SessionID,
		UserAuthVersion: identity.UserAuthVersion,
		SessionVersion:  identity.SessionVersion,
		Method:          method,
		Scopes:          []string{binding.Scope},
		ContextHash:     binding.ContextHash,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    authTokenIssuer,
			Subject:   strconv.Itoa(identity.UserID),
			Audience:  jwt.ClaimStrings{authTokenAudience},
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			NotBefore: jwt.NewNumericDate(now.Add(-5 * time.Second)),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        proofID,
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(SigningKey(securityProofTokenUse))
	return signed, expiresAt.Unix(), err
}

func VerifySecurityProof(raw string, identity domain.AuthIdentity, binding domain.VerificationBinding) (*authClaims, error) {
	claims, err := parseAuthClaims(raw, securityProofTokenUse, SigningKey(securityProofTokenUse))
	if err != nil {
		return nil, err
	}
	userID, err := strconv.Atoi(claims.Subject)
	if err != nil || userID != identity.UserID || claims.SessionID != identity.SessionID || claims.UserAuthVersion != identity.UserAuthVersion || claims.SessionVersion != identity.SessionVersion {
		return nil, domain.ErrAuthTokenInvalid
	}
	if len(claims.Scopes) != 1 || claims.Scopes[0] != binding.Scope {
		return nil, domain.ErrProofScope
	}
	if claims.ContextHash == "" {
		return nil, domain.ErrAuthTokenInvalid
	}
	if !hmac.Equal([]byte(claims.ContextHash), []byte(binding.ContextHash)) {
		return nil, domain.ErrProofContext
	}
	return claims, nil
}

func parseAuthClaims(raw, expectedUse string, key []byte) (*authClaims, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, domain.ErrAuthTokenInvalid
	}
	claims := &authClaims{}
	parsed, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("%w: unexpected signing method", domain.ErrAuthTokenInvalid)
		}
		return key, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuer(authTokenIssuer), jwt.WithAudience(authTokenAudience), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithLeeway(5*time.Second))
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, domain.ErrAuthTokenExpired
		}
		return nil, fmt.Errorf("%w: %v", domain.ErrAuthTokenInvalid, err)
	}
	if !parsed.Valid || claims.TokenUse != expectedUse || claims.ID == "" || claims.IssuedAt == nil || claims.NotBefore == nil {
		return nil, domain.ErrAuthTokenInvalid
	}
	return claims, nil
}
