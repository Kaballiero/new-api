package transport

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The expectations repeat the legacy responses of the account handlers:
// status, code and English message.
func TestWriteErrorKeepsLegacyResponses(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name string
		err  error
		want string
		code int
	}{
		{"missing", domain.ErrAccountNotFound, `{"success":false,"code":"user_not_found","message":"User does not exist"}`, http.StatusNotFound},
		{"manage forbidden", domain.ErrManageForbidden, `{"success":false,"code":"permission_denied","message":"No permission to update users of same or higher permission level"}`, http.StatusForbidden},
		{"view forbidden", domain.ErrViewForbidden, `{"success":false,"code":"permission_denied","message":"No permission to access users of same or higher level"}`, http.StatusForbidden},
		{"disable root", domain.ErrCannotDisableRoot, `{"success":false,"code":"cannot_disable_root","message":"Cannot disable super administrator user"}`, http.StatusForbidden},
		{"delete root", domain.ErrCannotDeleteRoot, `{"success":false,"code":"cannot_delete_root","message":"Cannot delete super administrator account"}`, http.StatusForbidden},
		{"promote", domain.ErrAdminCannotPromote, `{"success":false,"code":"admin_cannot_promote","message":"Regular administrators cannot promote other users to administrator"}`, http.StatusForbidden},
		{"already admin", domain.ErrAlreadyAdmin, `{"success":false,"code":"user_already_admin","message":"This user is already an administrator"}`, http.StatusConflict},
		{"demote root", domain.ErrCannotDemoteRoot, `{"success":false,"code":"cannot_demote_root","message":"Cannot demote super administrator user"}`, http.StatusForbidden},
		{"already common", domain.ErrAlreadyCommon, `{"success":false,"code":"user_already_common","message":"This user is already a common user"}`, http.StatusConflict},
		{"invalid action", domain.ErrInvalidAction, `{"success":false,"code":"invalid_params","message":"Invalid parameters"}`, http.StatusBadRequest},
		{"username taken", domain.ErrUsernameTaken, `{"success":false,"code":"username_already_taken","message":"Username already exists or has been deleted"}`, http.StatusConflict},
		{"quota zero", domain.ErrQuotaChangeZero, `{"success":false,"code":"quota_change_zero","message":"Quota change amount cannot be zero"}`, http.StatusBadRequest},
		{"quota invalid", domain.ErrQuotaAdjustmentInvalid, `{"success":false,"code":"invalid_params","message":"Invalid parameters"}`, http.StatusBadRequest},
		{"quota limit", domain.ErrQuotaLimitExceeded, `{"success":false,"message":"wallet quota limit exceeded"}`, http.StatusOK},
		{"internal", domain.Internal(errors.New("disk full")), `{"success":false,"code":"internal_error","message":"disk full"}`, http.StatusInternalServerError},
		{"group storage", domain.GroupChangeFailed(errors.New("disk full")), `{"success":false,"message":"disk full"}`, http.StatusInternalServerError},
		{"group invalid", domain.ErrInvalidGroupChange, `{"success":false,"message":"Invalid parameters"}`, http.StatusBadRequest},
		{"group batch", domain.ErrGroupBatchTooLarge, `{"success":false,"message":"Too many items in batch request, maximum is 1000"}`, http.StatusBadRequest},
		{"group missing", &domain.GroupError{Err: domain.ErrGroupDoesNotExist, Group: "vip"}, `{"success":false,"message":"Group 'vip' does not exist"}`, http.StatusBadRequest},
		{"group users missing", domain.ErrGroupUsersNotFound, `{"success":false,"message":"User does not exist"}`, http.StatusNotFound},
		{"group forbidden", domain.ErrGroupChangeForbidden, `{"success":false,"message":"No permission to update users of same or higher permission level"}`, http.StatusForbidden},
		{"role change", domain.ErrRoleChangeNotAllowed, `{"success":false,"message":"Invalid parameters"}`, http.StatusOK},
		{"profile invalid", domain.ErrProfileInvalid, `{"success":false,"message":"Invalid parameters"}`, http.StatusOK},
		{"profile input", domain.ErrProfileInputFailed, `{"success":false,"message":"Invalid input"}`, http.StatusOK},
		{"create forbidden", domain.ErrCreateForbidden, `{"success":false,"message":"Cannot create users with permission level equal to or higher than yourself"}`, http.StatusOK},
		{"binding forbidden", domain.ErrBindingForbidden, `{"success":false,"message":"No permission to access users of same or higher level"}`, http.StatusOK},
		{"checkin disabled", domain.ErrCheckinDisabled, `{"success":false,"message":"Check-in feature is not enabled"}`, http.StatusOK},
		{"checked in", domain.ErrAlreadyCheckedIn, `{"success":false,"message":"Already checked in today"}`, http.StatusOK},
		{"checkin failed", domain.ErrCheckinFailed, `{"success":false,"message":"Check-in failed, please try again later"}`, http.StatusOK},
		{"checkin quota", domain.ErrCheckinQuotaFailed, `{"success":false,"message":"Check-in failed: quota update error"}`, http.StatusOK},
		{"transfer minimum", domain.AffiliateTransferFailed(&domain.AmountError{Err: domain.ErrTransferBelowMinimum, Amount: "$1.00"}), `{"success":false,"message":"Transfer failed Minimum transfer quota is $1.00!"}`, http.StatusOK},
		{"transfer balance", domain.AffiliateTransferFailed(domain.ErrInviteQuotaInsufficient), `{"success":false,"message":"Transfer failed Invitation quota is insufficient!"}`, http.StatusOK},
		{"transfer storage", domain.AffiliateTransferFailed(errors.New("lock timeout")), `{"success":false,"message":"Transfer failed lock timeout"}`, http.StatusOK},
		{"settings save", domain.SettingsSaveFailed(errors.New("disk full")), `{"success":false,"message":"Update failed"}`, http.StatusOK},
		{"notify type", domain.ErrNotifyTypeInvalid, `{"success":false,"message":"Invalid warning type"}`, http.StatusOK},
		{"notify threshold", domain.ErrQuotaThresholdNotPositive, `{"success":false,"message":"Warning threshold must be greater than 0"}`, http.StatusOK},
		{"webhook empty", domain.ErrWebhookURLEmpty, `{"success":false,"message":"Webhook URL cannot be empty"}`, http.StatusOK},
		{"webhook invalid", domain.ErrWebhookURLInvalid, `{"success":false,"message":"Invalid Webhook URL"}`, http.StatusOK},
		{"email invalid", domain.ErrNotificationEmailInvalid, `{"success":false,"message":"Invalid email address"}`, http.StatusOK},
		{"bark empty", domain.ErrBarkURLEmpty, `{"success":false,"message":"Bark push URL cannot be empty"}`, http.StatusOK},
		{"bark invalid", domain.ErrBarkURLInvalid, `{"success":false,"message":"Invalid Bark push URL"}`, http.StatusOK},
		{"url scheme", domain.ErrNotificationURLNotHTTP, `{"success":false,"message":"URL must start with http:// or https://"}`, http.StatusOK},
		{"gotify url empty", domain.ErrGotifyURLEmpty, `{"success":false,"message":"Gotify server URL cannot be empty"}`, http.StatusOK},
		{"gotify token", domain.ErrGotifyTokenEmpty, `{"success":false,"message":"Gotify token cannot be empty"}`, http.StatusOK},
		{"gotify invalid", domain.ErrGotifyURLInvalid, `{"success":false,"message":"Invalid Gotify server URL"}`, http.StatusOK},
		{"plain", errors.New("lookup failed"), `{"success":false,"message":"lookup failed"}`, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			WriteError(c, tc.err)
			assert.Equal(t, tc.code, recorder.Code)
			assert.JSONEq(t, tc.want, recorder.Body.String())
		})
	}
}

// Previously hardcoded Chinese messages keep their text for zh-CN clients.
func TestWriteErrorLocalizesFormerChineseMessages(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	cases := []struct {
		err        error
		lang, want string
	}{
		{domain.ErrCheckinDisabled, "zh-CN", "签到功能未启用"},
		{domain.ErrAlreadyCheckedIn, "zh-CN", "今日已签到"},
		{domain.ErrCheckinFailed, "zh-CN", "签到失败，请稍后重试"},
		{domain.ErrCheckinQuotaFailed, "zh-CN", "签到失败：更新额度出错"},
		{domain.AffiliateTransferFailed(&domain.AmountError{Err: domain.ErrTransferBelowMinimum, Amount: "$1.00"}), "zh-CN", "划转失败 转移额度最小为$1.00！"},
		{domain.AffiliateTransferFailed(domain.ErrInviteQuotaInsufficient), "zh-CN", "划转失败 邀请额度不足！"},
		{domain.ErrAlreadyCheckedIn, "ru", "Вы уже отметились сегодня"},
	}
	for _, tc := range cases {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		c.Set(string(constant.ContextKeyLanguage), tc.lang)
		WriteError(c, tc.err)
		assert.Equal(t, http.StatusOK, recorder.Code)
		assert.JSONEq(t, `{"success":false,"message":"`+tc.want+`"}`, recorder.Body.String(), tc.want)
	}
}
