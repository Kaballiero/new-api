package identitycore

import (
	"errors"
	identitystorage "github.com/QuantumNous/new-api/internal/app/identitystorage"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	authtoken "github.com/QuantumNous/new-api/internal/adapter/identity/authtoken"
	identitydomain "github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useTestSessionSecret(t *testing.T) {
	t.Helper()
	previous := common.SessionSecret
	common.SessionSecret = "test-session-secret-with-sufficient-entropy"
	t.Cleanup(func() { common.SessionSecret = previous })
}

func TestAccessTokenRoundTripAndPurposeIsolation(t *testing.T) {
	setupAuthSessionTestDB(t)
	useTestSessionSecret(t)
	identity := identitydomain.AuthIdentity{UserID: 42, SessionID: "session-1", UserAuthVersion: 3, SessionVersion: 2}

	token, expiresAt, err := authtoken.IssueAccessToken(identity)
	require.NoError(t, err)
	assert.Positive(t, expiresAt)

	parsed, err := authtoken.ParseAccessToken(token)
	require.NoError(t, err)
	assert.Equal(t, identity, parsed)

	binding, err := BindVerificationOperation(identitydomain.VerificationOperation{Scope: "channel.key.read", Context: []byte(`{"channel_id":123}`)})
	require.NoError(t, err)
	proof, _, err := authtoken.IssueSecurityProof(identitystorage.SQL(), identity, "2fa", binding)
	require.NoError(t, err)
	_, err = authtoken.ParseAccessToken(proof)
	assert.ErrorIs(t, err, identitydomain.ErrAuthTokenInvalid)
}

func TestAccessTokenRejectsTampering(t *testing.T) {
	useTestSessionSecret(t)
	identity := identitydomain.AuthIdentity{UserID: 42, SessionID: "session-1", UserAuthVersion: 1, SessionVersion: 1}
	token, _, err := authtoken.IssueAccessToken(identity)
	require.NoError(t, err)

	tamperAt := len(token) - 2
	replacement := "x"
	if token[tamperAt] == 'x' {
		replacement = "y"
	}
	tampered := token[:tamperAt] + replacement + token[tamperAt+1:]
	_, err = authtoken.ParseAccessToken(tampered)
	assert.ErrorIs(t, err, identitydomain.ErrAuthTokenInvalid)

	_, internal, err := authtoken.ParseDashboardAccessToken(tampered)
	assert.True(t, internal)
	assert.ErrorIs(t, err, identitydomain.ErrAuthTokenInvalid)
}

func TestDashboardAccessTokenClassification(t *testing.T) {
	setupAuthSessionTestDB(t)
	useTestSessionSecret(t)

	identity, internal, err := authtoken.ParseDashboardAccessToken("opaque.key.with-dots")
	require.NoError(t, err)
	assert.False(t, internal)
	assert.Empty(t, identity)

	external := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": "external-issuer",
		"aud": authTokenAudience,
		"exp": time.Now().Add(time.Minute).Unix(),
	})
	externalRaw, err := external.SignedString([]byte("external-secret"))
	require.NoError(t, err)
	_, internal, err = authtoken.ParseDashboardAccessToken(externalRaw)
	require.NoError(t, err)
	assert.False(t, internal)

	unknownUse := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss":       authTokenIssuer,
		"aud":       authTokenAudience,
		"token_use": "third_party",
		"exp":       time.Now().Add(time.Minute).Unix(),
	})
	unknownUseRaw, err := unknownUse.SignedString([]byte("external-secret"))
	require.NoError(t, err)
	_, internal, err = authtoken.ParseDashboardAccessToken(unknownUseRaw)
	require.NoError(t, err)
	assert.False(t, internal)

	binding, err := BindVerificationOperation(identitydomain.VerificationOperation{Scope: "channel.key.read", Context: []byte(`{"channel_id":123}`)})
	require.NoError(t, err)
	proof, _, err := authtoken.IssueSecurityProof(identitystorage.SQL(), identitydomain.AuthIdentity{
		UserID: 42, SessionID: "session-1", UserAuthVersion: 1, SessionVersion: 1,
	}, "2fa", binding)
	require.NoError(t, err)
	_, internal, err = authtoken.ParseDashboardAccessToken(proof)
	assert.True(t, internal)
	assert.ErrorIs(t, err, identitydomain.ErrAuthTokenInvalid)

	expiredClaims := authClaims{
		TokenUse:        accessTokenUse,
		SessionID:       "expired-session",
		UserAuthVersion: 1,
		SessionVersion:  1,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    authTokenIssuer,
			Subject:   "42",
			Audience:  jwt.ClaimStrings{authTokenAudience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
			NotBefore: jwt.NewNumericDate(time.Now().Add(-2 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Minute)),
			ID:        "expired-token",
		},
	}
	expired, err := jwt.NewWithClaims(jwt.SigningMethodHS256, expiredClaims).SignedString(authtoken.SigningKey(accessTokenUse))
	require.NoError(t, err)
	_, internal, err = authtoken.ParseDashboardAccessToken(expired)
	assert.True(t, internal)
	assert.ErrorIs(t, err, identitydomain.ErrAuthTokenExpired)
}

func TestSecurityProofBindsIdentityAndOperation(t *testing.T) {
	setupAuthSessionTestDB(t)
	useTestSessionSecret(t)
	identity := identitydomain.AuthIdentity{UserID: 42, SessionID: "session-1", UserAuthVersion: 3, SessionVersion: 2}
	operation := identitydomain.VerificationOperation{Scope: "channel.key.read", Context: []byte(`{"channel_id":123}`)}
	binding, err := BindVerificationOperation(operation)
	require.NoError(t, err)
	proof, _, err := authtoken.IssueSecurityProof(identitystorage.SQL(), identity, "2fa", binding)
	require.NoError(t, err)

	claims, err := authtoken.VerifySecurityProof(proof, identity, binding)
	require.NoError(t, err)
	assert.Equal(t, "2fa", claims.Method)

	wrongScope, err := BindVerificationOperation(identitydomain.VerificationOperation{Scope: "passkey.delete"})
	require.NoError(t, err)
	_, err = authtoken.VerifySecurityProof(proof, identity, wrongScope)
	assert.ErrorIs(t, err, identitydomain.ErrProofScope)

	otherChannel, err := BindVerificationOperation(identitydomain.VerificationOperation{Scope: "channel.key.read", Context: []byte(`{"channel_id":456}`)})
	require.NoError(t, err)
	_, err = authtoken.VerifySecurityProof(proof, identity, otherChannel)
	assert.ErrorIs(t, err, identitydomain.ErrProofContext)

	otherSession := identity
	otherSession.SessionID = "session-2"
	_, err = authtoken.VerifySecurityProof(proof, otherSession, binding)
	assert.True(t, errors.Is(err, identitydomain.ErrAuthTokenInvalid))

	claims, err = authtoken.VerifySecurityProof(proof, identity, binding)
	require.NoError(t, err)
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute))
	expired, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(authtoken.SigningKey(securityProofTokenUse))
	require.NoError(t, err)
	_, err = ConsumeOperationProof(expired, identity, operation)
	assert.ErrorIs(t, err, identitydomain.ErrAuthTokenExpired)
}

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
