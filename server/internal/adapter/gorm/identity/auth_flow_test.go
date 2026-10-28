package identity_test

import (
	"errors"
	"testing"
	"time"

	identitystore "github.com/QuantumNous/new-api/internal/adapter/gorm/identity"
	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"
	identitystorage "github.com/QuantumNous/new-api/internal/app/identitystorage"
	identitydomain "github.com/QuantumNous/new-api/internal/identity/domain"
	model "github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAuthFlowIsBoundAndConsumedOnce(t *testing.T) {
	nativeFixture(t)

	token, created, err := identitystorage.SQL().CreateAuthFlow(identitystore.AuthFlowCreate{
		Purpose:   identitydomain.AuthFlowPurposeOAuth,
		Provider:  "github",
		Intent:    identitydomain.AuthFlowIntentBind,
		UserId:    42,
		SessionId: "session-a",
		Payload:   `{"affiliate_code":"invite"}`,
		ExpiresAt: time.Now().Add(time.Minute),
	})
	require.NoError(t, err)
	require.NotEmpty(t, token)
	assert.NotEqual(t, token, created.TokenHash)

	_, err = identitystorage.SQL().ConsumeAuthFlow(token, identitystore.AuthFlowMatch{
		Purpose:   identitydomain.AuthFlowPurposeOAuth,
		Provider:  "github",
		Intent:    identitydomain.AuthFlowIntentBind,
		UserId:    99,
		SessionId: "session-a",
	})
	assert.ErrorIs(t, err, identitydomain.ErrAuthFlowInvalid)

	peeked, err := identitystorage.SQL().GetAuthFlow(token, identitystore.AuthFlowMatch{Purpose: identitydomain.AuthFlowPurposeOAuth, Provider: "github"})
	require.NoError(t, err)
	assert.Nil(t, peeked.ConsumedAt)

	consumed, err := identitystorage.SQL().ConsumeAuthFlow(token, identitystore.AuthFlowMatch{
		Purpose:   identitydomain.AuthFlowPurposeOAuth,
		Provider:  "github",
		Intent:    identitydomain.AuthFlowIntentBind,
		UserId:    42,
		SessionId: "session-a",
	})
	require.NoError(t, err)
	require.NotNil(t, consumed.ConsumedAt)

	_, err = identitystorage.SQL().ConsumeAuthFlow(token, identitystore.AuthFlowMatch{Purpose: identitydomain.AuthFlowPurposeOAuth})
	assert.ErrorIs(t, err, identitydomain.ErrAuthFlowConsumed)
}

func TestAuthFlowExpiryIsEnforced(t *testing.T) {
	nativeFixture(t)

	token, flow, err := identitystorage.SQL().CreateAuthFlow(identitystore.AuthFlowCreate{
		Purpose:   identitydomain.AuthFlowPurposeTwoFALogin,
		UserId:    7,
		ExpiresAt: time.Now().Add(time.Minute),
	})
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(&identityrecord.AuthFlow{}).Where("id = ?", flow.Id).Update("expires_at", time.Now().Add(-time.Second)).Error)

	_, err = identitystorage.SQL().GetAuthFlow(token, identitystore.AuthFlowMatch{Purpose: identitydomain.AuthFlowPurposeTwoFALogin})
	assert.True(t, errors.Is(err, identitydomain.ErrAuthFlowExpired))
	_, err = identitystorage.SQL().ConsumeAuthFlow(token, identitystore.AuthFlowMatch{Purpose: identitydomain.AuthFlowPurposeTwoFALogin})
	assert.True(t, errors.Is(err, identitydomain.ErrAuthFlowExpired))
}

func TestExternalAuthAssertionCanOnlyBeClaimedOnce(t *testing.T) {
	nativeFixture(t)

	expiresAt := time.Now().Add(time.Minute)

	require.NoError(t, identitystorage.SQL().ClaimExternalAuthAssertion(identitydomain.AuthFlowPurposeTelegramAssertion, "signed-assertion", expiresAt))
	err := identitystorage.SQL().ClaimExternalAuthAssertion(identitydomain.AuthFlowPurposeTelegramAssertion, "signed-assertion", expiresAt)
	assert.ErrorIs(t, err, identitydomain.ErrAuthFlowConsumed)

	require.NoError(t, identitystorage.SQL().ClaimExternalAuthAssertion(identitydomain.AuthFlowPurposeTelegramAssertion, "different-assertion", expiresAt))
}

func TestConsumeAuthFlowWithActionRollsBackTogether(t *testing.T) {
	nativeFixture(t)

	token, _, err := identitystorage.SQL().CreateAuthFlow(identitystore.AuthFlowCreate{
		Purpose:   identitydomain.AuthFlowPurposeTelegramBind,
		UserId:    42,
		SessionId: "session-a",
		ExpiresAt: time.Now().Add(time.Minute),
	})
	require.NoError(t, err)
	actionErr := errors.New("binding failed")

	_, err = identitystorage.SQL().ConsumeAuthFlowWithAction(token, identitystore.AuthFlowMatch{
		Purpose: identitydomain.AuthFlowPurposeTelegramBind, UserId: 42, SessionId: "session-a",
	}, func(tx *gorm.DB, _ *identityrecord.AuthFlow) error {
		if err := identitystorage.SQL().ClaimExternalAuthAssertionWithTx(tx, identitydomain.AuthFlowPurposeTelegramAssertion, "assertion-a", time.Now().Add(time.Minute)); err != nil {
			return err
		}
		return actionErr
	})
	assert.ErrorIs(t, err, actionErr)

	flow, err := identitystorage.SQL().GetAuthFlow(token, identitystore.AuthFlowMatch{Purpose: identitydomain.AuthFlowPurposeTelegramBind})
	require.NoError(t, err)
	assert.Nil(t, flow.ConsumedAt)
	require.NoError(t, identitystorage.SQL().ClaimExternalAuthAssertion(identitydomain.AuthFlowPurposeTelegramAssertion, "assertion-a", time.Now().Add(time.Minute)))
}
