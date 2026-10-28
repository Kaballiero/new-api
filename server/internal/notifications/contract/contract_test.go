// Package contract pins real billing notification triggers without moving production code.
package contract

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	notification "github.com/QuantumNous/new-api/internal/adapter/notifications"
	"github.com/QuantumNous/new-api/internal/app/notifications"
	f "github.com/QuantumNous/new-api/internal/contracttest/managementfixture"
	h "github.com/QuantumNous/new-api/internal/contracttest/managementhttp"
	tf "github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
	"github.com/QuantumNous/new-api/model"
	httpclient "github.com/QuantumNous/new-api/pkg/httpclient"
	service "github.com/QuantumNous/new-api/relay/billing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func triggerCases() []string {
	names := []string{}
	for _, source := range []string{"wallet", "subscription"} {
		for _, channel := range []string{"email", "webhook", "bark", "gotify"} {
			for _, variant := range []string{"notify", "quiet", "threshold"} {
				names = append(names, source+"_"+channel+"_"+variant)
			}
		}
	}
	return names
}
func TestNotificationTriggersContract(t *testing.T) {
	f.Run(t, "TestNotificationTriggersContract", "triggers", "testdata/golden", triggerCases(),
		[]string{"wallet_webhook_notify", "subscription_webhook_notify"}, runTrigger)
}
func runTrigger(t *testing.T, name string) any {
	common.RedisEnabled = false
	constant.NotifyLimitCount = 20
	// Start the existing lazy janitor outside virtual time; no destination is used by this fixture event.
	require.NoError(t, notifications.Send(999999, "", notification.Preferences(dto.UserSetting{NotifyType: "fixture-unused"}), notification.Notice(dto.NewNotify("fixture-janitor", "", "", nil))))
	transport := &f.Transport{Requests: []f.Request{}, Reply: func(*http.Request, int) (int, string, error) { return 204, "", nil }}
	transport.EnableServer(t)
	return h.Run(t, false, func(t *testing.T, env *h.Fixture) any {
		parts := strings.Split(name, "_")
		source, channel, variant := parts[0], parts[1], parts[2]
		system_setting.WorkerUrl = ""
		system_setting.ServerAddress = "https://billing.fixture.invalid"
		system_setting.GetFetchSetting().EnableSSRFProtection = false
		operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeTokens

		httpclient.GetHttpClient().Transport = transport
		language := f.Locale()
		if language == "default" {
			language = ""
		}
		settings := dto.UserSetting{NotifyType: channel, Language: language, BillingPreference: "wallet_only",
			WebhookUrl: "https://notify.invalid/quota", BarkUrl: "https://notify.invalid/{{title}}/{{content}}",
			GotifyUrl: "https://notify.invalid", GotifyToken: "fixture-token", GotifyPriority: 0}
		common.QuotaRemindThreshold = 1000001
		if variant == "threshold" {
			common.QuotaRemindThreshold = 0
		}
		info := &relaycommon.RelayInfo{UserId: 1, TokenId: 101, TokenKey: "wave6ckey", UserQuota: 1000000,
			UserSetting: settings, UsingGroup: "default", UserGroup: "default", RequestId: "wave6c-trigger",
			OriginModelName: "alpha", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 21, ChannelType: 1}}
		var callErr error
		if source == "wallet" {
			callErr = service.PostConsumeQuota(info, 1, 0, variant != "quiet")
		} else {
			settings.BillingPreference = "subscription_only"
			info.UserSetting = settings
			plan := model.SubscriptionPlan{Id: 1, Title: "fixture-plan", Enabled: true, QuotaResetPeriod: "never", TotalAmount: 10}
			require.NoError(t, model.DB.Create(&plan).Error)
			require.NoError(t, model.DB.Create(&model.UserSubscription{Id: 1, UserId: 1, PlanId: 1,
				AmountTotal: 10, StartTime: 946684000, EndTime: 4102444800, Status: "active"}).Error)
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			require.Nil(t, service.PreConsumeBilling(context, 1, info))
			actual := 2
			if variant == "quiet" {
				actual = 0
			}
			callErr = service.New().SettleWss(context, info, actual)
		}
		require.NoError(t, callErr)
		synctest.Wait()
		if source == "wallet" && variant != "notify" {
			assert.Empty(t, transport.Requests)
		}
		if source == "wallet" && channel != "email" && variant == "notify" {
			require.Len(t, transport.Requests, 1)
		}
		extra := map[string]any{"outbound": transport.Requests, "subscription_source": info.BillingSource,
			"subscriptions": tf.Rows(t, model.DB, "user_subscriptions", "id,user_id,plan_id,amount_total,amount_used,status", "id"),
			"tokens":        tf.Rows(t, model.DB, "tokens", "id,remain_quota,used_quota", "id")}
		if info.BillingSource != "" {
			extra["source_kind"] = fmt.Sprint(info.BillingSource)
		}
		return env.Snapshot(t, extra)
	})
}
