package identity_test

import (
	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"
	accountscore "github.com/QuantumNous/new-api/internal/app/accountscore"
	identitystorage "github.com/QuantumNous/new-api/internal/app/identitystorage"
	identitydomain "github.com/QuantumNous/new-api/internal/identity/domain"
	model "github.com/QuantumNous/new-api/model"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestExternalIdentityClaimEnforcesSingleOwnerAtomically(t *testing.T) {
	nativeFixture(t)

	first := model.User{Username: "telegram-owner-one", Password: "password", AffCode: "telegram-owner-one"}
	second := model.User{Username: "telegram-owner-two", Password: "password", AffCode: "telegram-owner-two"}
	require.NoError(t, model.DB.Create(&first).Error)
	require.NoError(t, model.DB.Create(&second).Error)

	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
		return (identityrecord.ExternalIdentityClaim{}).Claim(tx, identityrecord.ExternalIdentityProviderTelegram, "telegram-123", first.Id)
	}))
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		return (identityrecord.ExternalIdentityClaim{}).Claim(tx, identityrecord.ExternalIdentityProviderTelegram, "telegram-123", second.Id)
	})
	assert.ErrorIs(t, err, identitydomain.ErrIdentityClaimed)

	err = model.DB.Transaction(func(tx *gorm.DB) error {
		return (identityrecord.ExternalIdentityClaim{}).Claim(tx, identityrecord.ExternalIdentityProviderTelegram, "telegram-456", first.Id)
	})
	assert.ErrorIs(t, err, identitydomain.ErrIdentityClaimed)

	var claims []identityrecord.ExternalIdentityClaim
	require.NoError(t, model.DB.Find(&claims).Error)
	require.Len(t, claims, 1)
	assert.Equal(t, first.Id, claims[0].UserId)
	assert.Equal(t, "telegram-123", claims[0].Subject)

	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
		return identitystorage.SQL().ReleaseExternalIdentityWithTx(tx, identityrecord.ExternalIdentityProviderTelegram, first.Id)
	}))
	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
		return (identityrecord.ExternalIdentityClaim{}).Claim(tx, identityrecord.ExternalIdentityProviderTelegram, "telegram-123", second.Id)
	}))
}

func TestClearTelegramBindingReleasesIdentityClaim(t *testing.T) {
	nativeFixture(t)

	user := model.User{Username: "telegram-unbind", Password: "password", TelegramId: "telegram-unbind-id"}
	require.NoError(t, model.DB.Create(&user).Error)
	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
		return (identityrecord.ExternalIdentityClaim{}).Claim(tx, identityrecord.ExternalIdentityProviderTelegram, user.TelegramId, user.Id)
	}))

	require.NoError(t, accountscore.ClearBinding(&user, identityrecord.ExternalIdentityProviderTelegram))
	assert.Empty(t, user.TelegramId)

	var count int64
	require.NoError(t, model.DB.Model(&identityrecord.ExternalIdentityClaim{}).Where("user_id = ?", user.Id).Count(&count).Error)
	assert.Zero(t, count)
}

func TestInitializeExternalIdentityClaimsIsIdempotent(t *testing.T) {
	nativeFixture(t)

	user := model.User{Username: "telegram-legacy", Password: "password", TelegramId: "telegram-legacy-id"}
	require.NoError(t, model.DB.Create(&user).Error)
	require.NoError(t, model.InitializeExternalIdentityClaims())
	require.NoError(t, model.InitializeExternalIdentityClaims())

	var claim identityrecord.ExternalIdentityClaim
	require.NoError(t, model.DB.Where("provider = ? AND subject = ?", identityrecord.ExternalIdentityProviderTelegram, user.TelegramId).
		First(&claim).Error)
	assert.Equal(t, user.Id, claim.UserId)
}

func TestInitializeExternalIdentityClaimsRejectsAmbiguousLegacyBindings(t *testing.T) {
	nativeFixture(t)

	first := model.User{Username: "telegram-legacy-one", Password: "password", TelegramId: "duplicate-telegram-id", AffCode: "telegram-legacy-one"}
	second := model.User{Username: "telegram-legacy-two", Password: "password", TelegramId: "duplicate-telegram-id", AffCode: "telegram-legacy-two"}
	require.NoError(t, model.DB.Create(&first).Error)
	require.NoError(t, model.DB.Create(&second).Error)

	err := model.InitializeExternalIdentityClaims()
	assert.ErrorIs(t, err, identitydomain.ErrIdentityClaimed)

	var count int64
	require.NoError(t, model.DB.Model(&identityrecord.ExternalIdentityClaim{}).Count(&count).Error)
	assert.Zero(t, count)
}
