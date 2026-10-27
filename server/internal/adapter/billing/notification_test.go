package billing_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	billingadapter "github.com/QuantumNous/new-api/internal/adapter/billing"
	notification "github.com/QuantumNous/new-api/internal/adapter/notifications"
	"github.com/QuantumNous/new-api/internal/app/notifications"
	httpclient "github.com/QuantumNous/new-api/pkg/httpclient"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type notificationTransport func(*http.Request) (*http.Response, error)

func (f notificationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestQuotaWebhookKeepsLegacyPayload(t *testing.T) {
	require.NoError(t, i18n.Init())
	httpclient.InitHttpClient()
	client := httpclient.GetHttpClient()
	previousTransport := client.Transport
	previousRedis := common.RedisEnabled
	previousLimit, previousDuration := constant.NotifyLimitCount, constant.NotificationLimitDurationMinute
	previousDisplay := operation_setting.GetGeneralSetting().QuotaDisplayType
	previousFetch := system_setting.GetFetchSetting().EnableSSRFProtection
	previousWorker, previousAddress := system_setting.WorkerUrl, system_setting.ServerAddress
	t.Cleanup(func() {
		client.Transport = previousTransport
		common.RedisEnabled = previousRedis
		constant.NotifyLimitCount, constant.NotificationLimitDurationMinute = previousLimit, previousDuration
		operation_setting.GetGeneralSetting().QuotaDisplayType = previousDisplay
		system_setting.GetFetchSetting().EnableSSRFProtection = previousFetch
		system_setting.WorkerUrl, system_setting.ServerAddress = previousWorker, previousAddress
	})
	common.RedisEnabled = false
	constant.NotifyLimitCount, constant.NotificationLimitDurationMinute = 10, 10
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeTokens
	system_setting.GetFetchSetting().EnableSSRFProtection = false
	system_setting.WorkerUrl, system_setting.ServerAddress = "", "https://billing.example"
	var payloads []map[string]any
	client.Transport = notificationTransport(func(r *http.Request) (*http.Response, error) {
		var payload map[string]any
		err := common.DecodeJson(r.Body, &payload)
		if err != nil {
			return nil, err
		}
		delete(payload, "timestamp")
		payloads = append(payloads, payload)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header)}, nil
	})
	settings := dto.UserSetting{NotifyType: "webhook", WebhookUrl: "https://fixture.invalid/quota", Language: "zh-CN"}
	const title = "您的额度即将用尽"
	const content = "{{value}}，当前剩余额度为 {{value}}，为了不影响您的使用，请及时充值。<br/>充值链接：<a href='{{value}}'>{{value}}</a>"
	values := []any{title, "123", "https://billing.example/wallet", "https://billing.example/wallet"}
	// Compare against the unchanged production renderer fed with the exact
	// legacy zh-CN template/values, without opening an HTTP listener or socket.
	require.NoError(t, notifications.Send(901231, "", notification.Preferences(settings), notification.Notice(dto.NewNotify(dto.NotifyTypeQuotaExceed, title, content, values))))
	require.NoError(t, (billingadapter.Notification{Send: notifications.Send}).Deliver(domain.Notice{Recipient: domain.Recipient{UserID: 901231, Language: "zh-CN", NotifyType: "webhook", WebhookURL: settings.WebhookUrl}, Remaining: 123}))
	require.Len(t, payloads, 2)
	assert.Equal(t, payloads[0], payloads[1])
	assert.Equal(t, []any{title, "123", "https://billing.example/wallet", "https://billing.example/wallet"}, payloads[1]["values"])
	for _, language := range []string{"en", "ru", ""} {
		require.NoError(t, (billingadapter.Notification{Send: notifications.Send}).Deliver(domain.Notice{Recipient: domain.Recipient{UserID: 901231, Language: language, NotifyType: "webhook", WebhookURL: settings.WebhookUrl}, Remaining: 123}))
		payload := payloads[len(payloads)-1]
		if language == "" {
			language = "en"
		}
		translated := i18n.Translate(language, i18n.MsgBillingWalletWarning)
		assert.NotEqual(t, i18n.MsgBillingWalletWarning, translated)
		assert.Equal(t, translated, payload["title"])
		assert.Equal(t, translated, payload["values"].([]any)[0])
	}
}
