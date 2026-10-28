package identity_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	cacheaccounts "github.com/QuantumNous/new-api/internal/adapter/cache/accounts"
	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"
	accountscore "github.com/QuantumNous/new-api/internal/app/accountscore"
	accountstorage "github.com/QuantumNous/new-api/internal/app/accountstorage"
	identitystorage "github.com/QuantumNous/new-api/internal/app/identitystorage"
	batchdomain "github.com/QuantumNous/new-api/internal/batch/domain"
	cachefixture "github.com/QuantumNous/new-api/internal/contracttest/cachefixture"
	identitydomain "github.com/QuantumNous/new-api/internal/identity/domain"
	model "github.com/QuantumNous/new-api/model"
	batchstate "github.com/QuantumNous/new-api/pkg/batchstate"

	"github.com/go-redis/redis/v8"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestHardDeleteUserFailsClosedWhenAuthFenceCannotPublish(t *testing.T) {
	nativeFixture(t)

	user := model.User{Username: "hard-delete-user", Password: "password", TelegramId: "hard-delete-telegram"}
	require.NoError(t, model.DB.Create(&user).Error)
	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
		return (identityrecord.ExternalIdentityClaim{}).Claim(tx, identityrecord.ExternalIdentityProviderTelegram, user.TelegramId, user.Id)
	}))
	require.NoError(t, model.DB.Create(&model.Token{UserId: user.Id, Key: "hard-delete-token"}).Error)
	require.NoError(t, model.DB.Create(&identityrecord.TwoFA{UserId: user.Id, Secret: "secret", IsEnabled: true}).Error)
	require.NoError(t, model.DB.Create(&identityrecord.TwoFABackupCode{UserId: user.Id, CodeHash: "hash"}).Error)
	require.NoError(t, model.DB.Create(&identityrecord.PasskeyCredential{UserID: user.Id, CredentialID: "credential", PublicKey: "public-key"}).Error)
	require.NoError(t, model.DB.Create(&identityrecord.UserOAuthBinding{UserId: user.Id, ProviderId: 1, ProviderUserId: "provider-user"}).Error)
	require.NoError(t, model.DB.Create(&identityrecord.UserSession{
		SID: "hard-delete-session", UserID: user.Id, Version: 1, UserAuthVersion: 1,
		Status: identityrecord.UserSessionStatusActive, RefreshHash: "refresh-hash", LoginMethod: "password",
		LastActiveAt: 1, ExpiresAt: 2,
	}).Error)
	require.NoError(t, model.DB.Create(&identityrecord.AuthFlow{
		TokenHash: "hard-delete-auth-flow", Purpose: identitydomain.AuthFlowPurposeTwoFALogin,
		UserId: user.Id, ExpiresAt: time.Now().Add(time.Minute),
	}).Error)

	oldRedisEnabled, oldRDB := common.RedisEnabled, common.RDB
	common.RedisEnabled = true
	common.RDB = redis.NewClient(&redis.Options{
		Dialer: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("forced redis failure")
		},
		MaxRetries: -1,
	})
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RedisEnabled, common.RDB = oldRedisEnabled, oldRDB
	})

	require.Error(t, accountscore.HardDeleteUserById(user.Id))

	var count int64
	require.NoError(t, model.DB.Unscoped().Model(&model.User{}).Where("id = ?", user.Id).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	for _, record := range []any{
		&model.Token{},
		&identityrecord.TwoFA{},
		&identityrecord.TwoFABackupCode{},
		&identityrecord.PasskeyCredential{},
		&identityrecord.UserOAuthBinding{},
		&identityrecord.UserSession{},
		&identityrecord.AuthFlow{},
		&identityrecord.ExternalIdentityClaim{},
	} {
		require.NoError(t, model.DB.Unscoped().Model(record).Where("user_id = ?", user.Id).Count(&count).Error)
		assert.EqualValues(t, 1, count)
	}
}

func TestHardDeleteUserPublishesTombstoneAndPurgesAuthenticationData(t *testing.T) {
	nativeFixture(t)

	server := cachefixture.NewRedis(t)

	user := model.User{
		Username: "hard-delete-success", Password: "password", AuthVersion: 1,
		TelegramId: "hard-delete-success-telegram",
	}
	require.NoError(t, model.DB.Create(&user).Error)
	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
		return (identityrecord.ExternalIdentityClaim{}).Claim(tx, identityrecord.ExternalIdentityProviderTelegram, user.TelegramId, user.Id)
	}))
	require.NoError(t, model.DB.Create(&model.Token{UserId: user.Id, Key: "hard-delete-success-token"}).Error)
	require.NoError(t, model.DB.Create(&identityrecord.TwoFA{UserId: user.Id, Secret: "secret", IsEnabled: true}).Error)
	require.NoError(t, model.DB.Create(&identityrecord.TwoFABackupCode{UserId: user.Id, CodeHash: "hash"}).Error)
	require.NoError(t, model.DB.Create(&identityrecord.PasskeyCredential{UserID: user.Id, CredentialID: "credential-success", PublicKey: "public-key"}).Error)
	require.NoError(t, model.DB.Create(&identityrecord.UserOAuthBinding{UserId: user.Id, ProviderId: 1, ProviderUserId: "provider-user-success"}).Error)
	require.NoError(t, model.DB.Create(&identityrecord.UserSession{
		SID: "hard-delete-success-session", UserID: user.Id, Version: 1, UserAuthVersion: 1,
		Status: identityrecord.UserSessionStatusActive, RefreshHash: "refresh-hash", LoginMethod: "password",
		LastActiveAt: 1, ExpiresAt: 2,
	}).Error)
	require.NoError(t, model.DB.Create(&identityrecord.AuthFlow{
		TokenHash: "hard-delete-success-flow", Purpose: identitydomain.AuthFlowPurposeTwoFALogin,
		UserId: user.Id, ExpiresAt: time.Now().Add(time.Minute),
	}).Error)
	require.NoError(t, accountstorage.Cache().PopulateUserCache(user))
	// Administrative hard deletion commonly targets an already soft-deleted
	// user; the shared version increment must therefore query unscoped.
	require.NoError(t, model.DB.Delete(&user).Error)

	require.NoError(t, accountscore.HardDeleteUserById(user.Id))

	var count int64
	require.NoError(t, model.DB.Unscoped().Model(&model.User{}).Where("id = ?", user.Id).Count(&count).Error)
	assert.Zero(t, count)
	for _, record := range []any{
		&model.Token{},
		&identityrecord.TwoFA{},
		&identityrecord.TwoFABackupCode{},
		&identityrecord.PasskeyCredential{},
		&identityrecord.UserOAuthBinding{},
		&identityrecord.UserSession{},
		&identityrecord.AuthFlow{},
		&identityrecord.ExternalIdentityClaim{},
	} {
		require.NoError(t, model.DB.Unscoped().Model(record).Where("user_id = ?", user.Id).Count(&count).Error)
		assert.Zero(t, count)
	}
	assert.False(t, server.Exists(fmt.Sprintf("auth:user:fence:%d", user.Id)))
	committed, err := common.RDB.Get(t.Context(), fmt.Sprintf("auth:user:version:%d", user.Id)).Result()
	require.NoError(t, err)
	assert.Equal(t, "2", committed)
	assert.False(t, server.Exists(cacheaccounts.GetUserCacheKey(user.Id)))
}

func TestIncrementFailedAttemptsCountsConcurrentFailures(t *testing.T) {
	nativeFixture(t)

	user := model.User{Username: "twofa-cas-user", Password: "password"}
	require.NoError(t, model.DB.Create(&user).Error)
	twoFA := identityrecord.TwoFA{UserId: user.Id, Secret: "secret", IsEnabled: true}
	require.NoError(t, model.DB.Create(&twoFA).Error)

	const attempts = 4
	errs := make(chan error, attempts)
	var wg sync.WaitGroup
	for range attempts {
		wg.Go(func() {
			errs <- identitystorage.SQL().IncrementFailedAttempts(&identityrecord.TwoFA{Id: twoFA.Id})
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	var reloaded identityrecord.TwoFA
	require.NoError(t, model.DB.First(&reloaded, twoFA.Id).Error)
	assert.Equal(t, attempts, reloaded.FailedAttempts)
}

func TestValidateBackupCodeCanOnlySucceedOnce(t *testing.T) {
	nativeFixture(t)

	const code = "ABCD-1234"
	user := model.User{Id: 123, Username: "backup-code-user", Password: "password", AuthVersion: 1}
	require.NoError(t, model.DB.Create(&user).Error)
	require.NoError(t, model.DB.Create(&identityrecord.TwoFA{UserId: user.Id, Secret: "secret", IsEnabled: false}).Error)
	hashedCode, err := common.HashBackupCode(code)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&identityrecord.TwoFABackupCode{UserId: user.Id, CodeHash: hashedCode}).Error)

	const attempts = 2
	results := make(chan bool, attempts)
	errs := make(chan error, attempts)
	var wg sync.WaitGroup
	for range attempts {
		wg.Go(func() {
			valid, err := identitystorage.SQL().ValidateBackupCode(123, code)
			results <- valid
			errs <- err
		})
	}
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
	wins := 0
	for valid := range results {
		if valid {
			wins++
		}
	}
	assert.Equal(t, 1, wins)

	remaining, err := identitystorage.SQL().GetUnusedBackupCodeCount(123)
	require.NoError(t, err)
	assert.Zero(t, remaining)
}

func TestPendingTwoFASetupAPIsRejectEnabledFactor(t *testing.T) {
	nativeFixture(t)

	user := model.User{Username: "enabled-twofa-guard", Password: "password", Status: common.UserStatusEnabled, AuthVersion: 1}
	require.NoError(t, model.DB.Create(&user).Error)
	twoFA := identityrecord.TwoFA{UserId: user.Id, Secret: "secret", IsEnabled: true}
	require.NoError(t, model.DB.Create(&twoFA).Error)

	session := identityrecord.UserSession{SID: "enabled-factor-session", UserID: user.Id, Version: 1, UserAuthVersion: 1, Status: identityrecord.UserSessionStatusActive, ExpiresAt: time.Now().Add(time.Hour).Unix()}
	require.NoError(t, model.DB.Create(&session).Error)
	identity := identitydomain.AuthIdentity{UserID: user.Id, SessionID: session.SID, UserAuthVersion: 1, SessionVersion: 1}
	authorization := &identitydomain.FlowAuthorization{AuthSessionIdentity: identity, ProofID: 1, Scope: "2fa.setup", ContextHash: "setup-context", Method: "password"}
	_, err := identitystorage.SQL().CreateTwoFAEnrollment(identity, authorization, "replacement-secret", []string{"ABCD-1234"}, time.Now().Add(time.Minute))
	require.ErrorIs(t, err, identitydomain.ErrTwoFAAlreadyEnabled)

	var stored identityrecord.TwoFA
	require.NoError(t, model.DB.First(&stored, twoFA.Id).Error)
	assert.True(t, stored.IsEnabled)
	var backupCodeCount int64
	require.NoError(t, model.DB.Model(&identityrecord.TwoFABackupCode{}).Where("user_id = ?", user.Id).Count(&backupCodeCount).Error)
	assert.Zero(t, backupCodeCount)
}

func TestSecurityFactorMutationsAdvanceUserAuthVersion(t *testing.T) {
	nativeFixture(t)

	user := model.User{
		Username:    "security-factor-version-user",
		Password:    "password",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AuthVersion: 1,
	}
	require.NoError(t, model.DB.Create(&user).Error)
	session := identityrecord.UserSession{SID: "factor-version-session", UserID: user.Id, Version: 1, UserAuthVersion: 1, Status: identityrecord.UserSessionStatusActive, ExpiresAt: time.Now().Add(time.Hour).Unix()}
	require.NoError(t, model.DB.Create(&session).Error)
	identity := identitydomain.AuthIdentity{UserID: user.Id, SessionID: session.SID, UserAuthVersion: 1, SessionVersion: 1}
	authorization := &identitydomain.FlowAuthorization{AuthSessionIdentity: identity, ProofID: 1, Scope: "2fa.setup", ContextHash: "setup-context", Method: "password"}
	token, err := identitystorage.SQL().CreateTwoFAEnrollment(identity, authorization, "JBSWY3DPEHPK3PXP", []string{"ABCD-1234"}, time.Now().Add(time.Minute))
	require.NoError(t, err)
	code, err := totp.GenerateCode("JBSWY3DPEHPK3PXP", time.Now())
	require.NoError(t, err)
	require.NoError(t, identitystorage.SQL().EnableTwoFAEnrollment(identity, token, code))
	assertUserAuthVersion(t, user.Id, 2)
	assert.ErrorIs(t, identitystorage.SQL().EnableTwoFAEnrollment(identity, token, code), identitydomain.ErrTwoFASetupInvalid)
	assertUserAuthVersion(t, user.Id, 2)
	require.NoError(t, identitystorage.SQL().ReplaceBackupCodesWithAuthVersion(user.Id, []string{"ABCD-1234"}))
	assertUserAuthVersion(t, user.Id, 3)
	require.NoError(t, identitystorage.SQL().DisableTwoFAWithAuthVersion(user.Id))
	assertUserAuthVersion(t, user.Id, 4)

	credential := &identityrecord.PasskeyCredential{UserID: user.Id, CredentialID: "credential-id", PublicKey: "public-key"}
	require.NoError(t, identitystorage.SQL().UpsertPasskeyCredentialWithAuthVersion(credential))
	assertUserAuthVersion(t, user.Id, 5)
	require.NoError(t, identitystorage.SQL().DeletePasskeyByUserIDWithAuthVersion(user.Id))
	assertUserAuthVersion(t, user.Id, 6)
	for _, tc := range []struct {
		name, table string
		create      bool
		run         func(int) error
	}{
		{"backup_replace", "two_fa_backup_codes", true, func(id int) error {
			return identitystorage.SQL().ReplaceBackupCodesWithAuthVersion(id, []string{"ABCD-1234"})
		}},
		{"twofa_disable", "two_fas", false, identitystorage.SQL().DisableTwoFAWithAuthVersion},
		{"passkey_upsert", "passkey_credentials", true, func(id int) error {
			return identitystorage.SQL().UpsertPasskeyCredentialWithAuthVersion(&identityrecord.PasskeyCredential{UserID: id, CredentialID: "replacement", PublicKey: "replacement"})
		}},
		{"passkey_delete", "passkey_credentials", false, identitystorage.SQL().DeletePasskeyByUserIDWithAuthVersion},
	} {
		t.Run("rollback_"+tc.name, func(t *testing.T) {
			nativeFixture(t)
			user := model.User{Id: 41, Username: "factor-rollback", Password: "unused", AuthVersion: 1, Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
			require.NoError(t, model.DB.Create(&user).Error)
			factor := identityrecord.TwoFA{UserId: user.Id, Secret: "original", IsEnabled: true}
			backup := identityrecord.TwoFABackupCode{UserId: user.Id, CodeHash: "original"}
			credential := identityrecord.PasskeyCredential{UserID: user.Id, CredentialID: "original", PublicKey: "original"}
			require.NoError(t, model.DB.Create(&factor).Error)
			require.NoError(t, model.DB.Create(&backup).Error)
			require.NoError(t, model.DB.Create(&credential).Error)
			require.NoError(t, model.DB.First(&factor, factor.Id).Error)
			require.NoError(t, model.DB.First(&backup, backup.Id).Error)
			require.NoError(t, model.DB.First(&credential, credential.ID).Error)
			forced := errors.New("forced factor write failure")
			hits := 0
			callbacks, event := model.DB.Callback().Delete(), "gorm:delete"
			if tc.create {
				callbacks, event = model.DB.Callback().Create(), "gorm:create"
			}
			require.NoError(t, callbacks.Before(event).Register("factor_rollback", func(tx *gorm.DB) {
				if tx.Statement.Table == tc.table {
					hits++
					tx.AddError(forced)
				}
			}))
			t.Cleanup(func() { require.NoError(t, callbacks.Remove("factor_rollback")) })
			err := tc.run(user.Id)
			if tc.name == "passkey_upsert" {
				require.EqualError(t, err, "passkey save failed")
			} else {
				require.ErrorIs(t, err, forced)
			}
			require.Equal(t, 1, hits)
			assertUserAuthVersion(t, user.Id, 1)
			var storedFactor identityrecord.TwoFA
			var storedBackup identityrecord.TwoFABackupCode
			var storedCredential identityrecord.PasskeyCredential
			require.NoError(t, model.DB.First(&storedFactor, factor.Id).Error)
			require.NoError(t, model.DB.First(&storedBackup, backup.Id).Error)
			require.NoError(t, model.DB.First(&storedCredential, credential.ID).Error)
			assert.Equal(t, factor, storedFactor)
			assert.Equal(t, backup, storedBackup)
			assert.Equal(t, credential, storedCredential)
		})
	}
}

func TestUpdatePasskeyAssertionStateCannotRewriteRegistrationIdentity(t *testing.T) {
	nativeFixture(t)

	user := model.User{Username: "passkey-assertion-state", Password: "password", AuthVersion: 1}
	require.NoError(t, model.DB.Create(&user).Error)
	credentialID := []byte("stable-credential-id")
	stored := identityrecord.PasskeyCredential{
		UserID:          user.Id,
		CredentialID:    base64.StdEncoding.EncodeToString(credentialID),
		PublicKey:       "original-public-key",
		AttestationType: "packed",
		AAGUID:          "original-aaguid",
		SignCount:       1,
		Transports:      `["usb"]`,
		Attachment:      "platform",
	}
	require.NoError(t, model.DB.Create(&stored).Error)
	usedAt := time.Now().UTC().Truncate(time.Second)
	validated := &webauthn.Credential{
		ID:              credentialID,
		PublicKey:       []byte("replacement-public-key"),
		AttestationType: "none",
		Flags: webauthn.CredentialFlags{
			UserPresent:    true,
			UserVerified:   true,
			BackupEligible: true,
			BackupState:    true,
		},
		Authenticator: webauthn.Authenticator{
			AAGUID:       []byte("replacement-aaguid"),
			SignCount:    8,
			CloneWarning: true,
		},
	}
	require.NoError(t, identitystorage.SQL().UpdatePasskeyAssertionState(user.Id, validated, usedAt))

	var updated identityrecord.PasskeyCredential
	require.NoError(t, model.DB.First(&updated, stored.ID).Error)
	assert.Equal(t, stored.CredentialID, updated.CredentialID)
	assert.Equal(t, stored.PublicKey, updated.PublicKey)
	assert.Equal(t, stored.AttestationType, updated.AttestationType)
	assert.Equal(t, stored.AAGUID, updated.AAGUID)
	assert.Equal(t, stored.Transports, updated.Transports)
	assert.Equal(t, stored.Attachment, updated.Attachment)
	assert.EqualValues(t, 8, updated.SignCount)
	assert.True(t, updated.CloneWarning)
	assert.True(t, updated.UserPresent)
	assert.True(t, updated.UserVerified)
	assert.True(t, updated.BackupEligible)
	assert.True(t, updated.BackupState)
	require.NotNil(t, updated.LastUsedAt)
	assert.Equal(t, usedAt.Unix(), updated.LastUsedAt.Unix())

	validated.ID = []byte("another-credential")
	assert.ErrorIs(t, identitystorage.SQL().UpdatePasskeyAssertionState(user.Id, validated, usedAt), identitydomain.ErrPasskeyNotFound)
}

func assertUserAuthVersion(t *testing.T, userID int, expected int64) {
	t.Helper()
	var version int64
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", userID).Select("auth_version").Scan(&version).Error)
	assert.Equal(t, expected, version)
}

func TestHardDeleteUserBatchConflictKeepsSentinelIdentity(t *testing.T) {
	nativeFixture(t)

	user := model.User{Username: "hard-delete-batch-conflict", Password: "unused", Quota: 100}
	require.NoError(t, model.DB.Create(&user).Error)
	job := model.BatchJob{PublicID: "hard-delete-batch", UserID: user.Id, ReservedQuota: 1, BillingStatus: "reserved", PurchaseStatus: "pending"}
	require.NoError(t, model.DB.Create(&job).Error)
	t.Cleanup(func() { require.NoError(t, model.DB.Delete(&model.BatchJob{}, job.ID).Error) })
	err := accountscore.HardDeleteUserById(user.Id)
	require.ErrorIs(t, err, batchstate.ErrConflict)
	require.ErrorIs(t, err, batchdomain.ErrBatchConflict)
}
