package notifications_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/textproto"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	notificationadapter "github.com/QuantumNous/new-api/internal/adapter/notifications"
	redisnotification "github.com/QuantumNous/new-api/internal/adapter/redis/notifications"
	"github.com/QuantumNous/new-api/internal/app/channelruntime"
	"github.com/QuantumNous/new-api/internal/app/notifications"
	"github.com/QuantumNous/new-api/internal/channels/disable_channel"
	"github.com/QuantumNous/new-api/service"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	f "github.com/QuantumNous/new-api/internal/contracttest/managementfixture"
	tf "github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func init() { tf.RegisterUpdate(); tf.RejectCandidateUpdate() }
func notificationCases() []string {
	names := []string{"root_missing", "watchers", "watchers_disabled", "watchers_query_fault",
		"watchers_partial_failure", "root_channel_disabled", "root_channel_enabled", "root_channel_autoban_off",
		"memory_limit", "memory_expiry", "memory_hours", "memory_event_keys", "memory_cleanup_loop",
		"redis_missing", "redis_current", "redis_limit", "redis_malformed", "redis_get_error",
		"redis_set_error", "redis_ttl_error", "redis_pipeline_error", "redis_no_ttl", "email_smtp",
		"email_override", "email_no_config", "email_bad_from", "webhook_values_nil", "webhook_values_empty",
		"webhook_fmt_legacy", "webhook_signature"}
	for _, channel := range []string{"email", "webhook", "bark", "gotify", "", "unknown"} {
		name := channel
		if name == "" {
			name = "default"
		}
		for _, variant := range []string{"success", "missing", "limit", "non2xx", "network", "worker", "ssrf_reject"} {
			if (channel == "email" || channel == "") && variant != "missing" && variant != "limit" {
				continue
			}
			if channel == "unknown" && variant != "success" && variant != "limit" {
				continue
			}
			names = append(names, "delivery_"+name+"_"+variant)
		}
	}
	for _, priority := range []string{"-1", "0", "10", "11"} {
		names = append(names, "gotify_priority_"+priority)
	}
	for _, language := range []string{"en", "ru", "zh-CN", "zh-TW", "default"} {
		names = append(names, "root_language_"+language)
	}
	slices.Sort(names)
	return names
}
func TestNotificationsContract(t *testing.T) {
	f.Run(t, "TestNotificationsContract", "notifications", "../../../service/testdata/notifications",
		notificationCases(), []string{"delivery_default_missing", "webhook_fmt_legacy", "root_language_ru"},
		runNotification)
}
func runNotification(t *testing.T, name string) any {
	*notificationadapter.NotifyLimitStore = sync.Map{}
	tf.InstallDBClock(t)
	tf.Configure()
	require.NoError(t, i18n.Init())
	t.Setenv("SQL_DSN", "")
	t.Setenv("LOG_SQL_DSN", "")
	common.SQLitePath = t.TempDir() + "/notify.db?_pragma=synchronous(OFF)"
	common.IsMasterNode = true
	require.NoError(t, model.InitDB())
	require.NoError(t, model.InitLogDB())
	service.InitHttpClient()
	service.GetChannelAffinityCacheStats()
	// Delivery cases characterize the limiter separately; its original janitor is outside the virtual clock.
	if name != "memory_cleanup_loop" {
		notificationadapter.CleanupOnce.Do(notificationadapter.StartCleanupTask)
	}
	transport := &f.Transport{Requests: []f.Request{}, Reply: func(*http.Request, int) (int, string, error) {
		if strings.HasSuffix(name, "_network") {
			return 0, "", errors.New("fixture send failure")
		}
		if strings.HasSuffix(name, "_non2xx") {
			return 503, "fixture failure", nil
		}
		return 204, "", nil
	}}
	transport.EnableServer(t)
	var result any
	synctest.Test(t, func(t *testing.T) {
		constant.NotifyLimitCount, constant.NotificationLimitDurationMinute = 20, 10
		common.OptionMap = map[string]string{}
		system_setting.WorkerUrl = ""
		system_setting.WorkerValidKey = "fixture-worker-key"
		system_setting.GetFetchSetting().EnableSSRFProtection = false
		notification := dto.NewNotify(dto.NotifyTypeQuotaExceed, "Fixture title + &", "first {{value}}, second {{value}}", []any{"A & +", "B"})
		settings := dto.UserSetting{NotifyType: "webhook", WebhookUrl: "https://notify.invalid/hook",
			WebhookSecret: "fixture-signing-secret", BarkUrl: "https://notify.invalid/{{title}}/{{content}}",
			GotifyUrl: "https://notify.invalid/", GotifyToken: "fixture-gotify-token", GotifyPriority: 0,
			Language: "zh-CN"}

		service.GetHttpClient().Transport = transport
		service.GetSSRFProtectedHTTPClient().Transport = transport
		var logs bytes.Buffer
		oldWriter, oldErrorWriter := gin.DefaultWriter, gin.DefaultErrorWriter
		common.LogWriterMu.Lock()
		gin.DefaultWriter = &logs
		gin.DefaultErrorWriter = &logs
		common.LogWriterMu.Unlock()
		t.Cleanup(func() {
			common.LogWriterMu.Lock()
			gin.DefaultWriter = oldWriter
			gin.DefaultErrorWriter = oldErrorWriter
			common.LogWriterMu.Unlock()
		})
		outcomes := []any{}
		extra := map[string]any{}
		capture := func(err error) {
			message := ""
			if err != nil {
				message = err.Error()
			}
			outcomes = append(outcomes, message)
		}
		switch {
		case strings.HasPrefix(name, "redis_"):
			hook := &notificationRedis{variant: strings.TrimPrefix(name, "redis_"), values: map[string]string{},
				expiration: map[string]time.Duration{}, calls: [][]string{}}
			key := "notify_limit:7:quota_exceed:2000010100"
			if hook.variant != "missing" && hook.variant != "set_error" {
				hook.values[key] = "1"
				hook.expiration[key] = time.Minute * 8
			}
			if hook.variant == "limit" {
				hook.values[key] = "20"
			}
			if hook.variant == "malformed" {
				hook.values[key] = "bad"
			}
			if hook.variant == "no_ttl" {
				hook.expiration[key] = -1
			}
			client := redis.NewClient(&redis.Options{Addr: "forbidden.invalid:0", MaxRetries: -1, Dialer: func(context.Context, string, string) (net.Conn, error) {
				return nil, errors.New("unexpected Redis dial")
			}})
			client.AddHook(hook)
			common.RDB, common.RedisEnabled = client, true
			allowed, err := (redisnotification.Counter{}).Check(7, dto.NotifyTypeQuotaExceed)
			capture(err)
			outcomes = append(outcomes, allowed)
			common.RedisEnabled = false
			require.NoError(t, client.Close())
			extra["redis_commands"], extra["redis_values"], extra["redis_ttl"] = hook.calls, hook.values, hook.expiration
		case strings.HasPrefix(name, "memory_"):
			constant.NotifyLimitCount = 2
			if name == "memory_cleanup_loop" {
				allowed, err := (notificationadapter.MemoryCounter{}).Check(7, dto.NotifyTypeQuotaExceed), error(nil)
				require.NoError(t, err)
				require.True(t, allowed)
				synctest.Wait()
				notificationadapter.NotifyLimitStore.Store("expired", notificationadapter.LimitCount{Count: 3, Timestamp: time.Now()})
				time.Sleep(time.Hour - time.Second)
				synctest.Wait()
				_, before := notificationadapter.NotifyLimitStore.Load("expired")
				time.Sleep(time.Second)
				synctest.Wait()
				_, after := notificationadapter.NotifyLimitStore.Load("expired")
				extra["before_hour"], extra["after_hour"] = before, after
				assert.True(t, before)
				assert.False(t, after)
			} else {
				for i := range 4 {
					event := dto.NotifyTypeQuotaExceed
					if name == "memory_event_keys" && i > 1 {
						event = dto.NotifyTypeChannelTest
					}
					allowed, err := (notificationadapter.MemoryCounter{}).Check(7, event), error(nil)
					capture(err)
					outcomes = append(outcomes, allowed)
				}
				if name == "memory_expiry" {
					time.Sleep(10 * time.Minute)
					allowed, err := (notificationadapter.MemoryCounter{}).Check(7, dto.NotifyTypeQuotaExceed), error(nil)
					capture(err)
					outcomes = append(outcomes, allowed)
				}
				if name == "memory_hours" {
					time.Sleep(time.Hour)
					allowed, err := (notificationadapter.MemoryCounter{}).Check(7, dto.NotifyTypeQuotaExceed), error(nil)
					capture(err)
					outcomes = append(outcomes, allowed)
				}
			}
		case strings.HasPrefix(name, "watchers") || strings.HasPrefix(name, "root_"):
			seedNotificationRecipients(t, settings)
			if name == "root_missing" {
				require.NoError(t, model.DB.Unscoped().Where("role=?", common.RoleRootUser).Delete(&model.User{}).Error)
			}
			if name == "watchers_disabled" {
				require.NoError(t, model.DB.Model(&model.User{}).Where("role>=?", common.RoleAdminUser).Update("setting",
					`{"notify_type":"webhook","upstream_model_update_notify_enabled":false}`).Error)
			}
			if name == "watchers_query_fault" {
				require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register("notify-query-fault", func(tx *gorm.DB) {
					if tx.Statement.Table == "users" {
						tx.AddError(errors.New("fixture recipient query failure"))
					}
				}))
			}
			if name == "watchers_partial_failure" {
				transport.Reply = func(r *http.Request, _ int) (int, string, error) {
					if strings.Contains(r.URL.Path, "admin") {
						return 503, "", nil
					}
					return 204, "", nil
				}
			}
			if strings.HasPrefix(name, "root_language_") {
				language := strings.TrimPrefix(name, "root_language_")
				if language == "default" {
					language = ""
				}
				settings.Language = language
				raw, err := common.Marshal(settings)
				require.NoError(t, err)
				require.NoError(t, model.DB.Model(&model.User{}).Where("id=3").Update("setting", string(raw)).Error)
				notifications.Root(dto.NotifyTypeChannelTest, "already rendered title", "already rendered content")
			} else if strings.HasPrefix(name, "root_channel_") {
				channel := model.Channel{Id: 21, Name: "fixture channel", Type: 1, Key: "fixture", Status: 1, Group: "default", Models: "alpha"}
				if name == "root_channel_enabled" {
					channel.Status = common.ChannelStatusAutoDisabled
				}
				require.NoError(t, channel.Insert())
				if name == "root_channel_enabled" {
					channelruntime.Enable(21, "", "fixture channel")
				} else {
					channelruntime.Disable(disable_channel.Input{ChannelID: 21, ChannelName: "fixture channel", AutoBan: name != "root_channel_autoban_off"}, "fixture reason")
				}
			} else if strings.HasPrefix(name, "watchers") {
				notifications.ModelUpdateWatchers("Root rendered title", "Root rendered content")
			} else {
				notifications.Root(dto.NotifyTypeChannelTest, "fixture title", "fixture content")
			}
		case strings.HasPrefix(name, "email_"):
			settings.NotifyType = "email"
			common.SMTPFrom = "fixture@example.invalid"
			common.SMTPAccount = ""
			common.SMTPServer = ""
			common.SMTPToken = ""
			switch name {
			case "email_bad_from":
				common.SMTPFrom = "bad"
			case "email_smtp", "email_override":
				address, messages := notificationSMTP(t)
				host, port, err := net.SplitHostPort(address)
				require.NoError(t, err)
				common.SMTPServer = host
				common.SMTPPort, err = strconv.Atoi(port)
				require.NoError(t, err)
				common.SMTPSSLEnabled = false
				common.SMTPStartTLSEnabled = false
				if name == "email_override" {
					settings.NotificationEmail = "override@example.invalid;second@example.invalid"
				}
				capture(sendFixtureNotification(7, "account@example.invalid", settings, notification))
				raw := <-messages
				require.Regexp(t, `Message-ID: <946684800[0-9]{9}\.[A-Za-z0-9]{12}@example.invalid>`, raw)
				raw = regexp.MustCompile(`Message-ID: <[^\r\n]+>`).ReplaceAllString(raw, "Message-ID: <fixture-message@example.invalid>")
				extra["smtp_message"] = raw
			}
			if name == "email_no_config" || name == "email_bad_from" {
				capture(sendFixtureNotification(7, "account@example.invalid", settings, notification))
			}
		default:
			if strings.HasPrefix(name, "delivery_") {
				rest := strings.TrimPrefix(name, "delivery_")
				channel, variant, _ := strings.Cut(rest, "_")
				settings.NotifyType = channel
				if channel == "default" {
					settings.NotifyType = ""
				}
				if variant == "worker" {
					system_setting.WorkerUrl = "https://worker.invalid/api"
				}
				if variant == "missing" {
					settings.WebhookUrl = ""
					settings.BarkUrl = ""
					settings.GotifyToken = ""
				}
				if variant == "limit" {
					constant.NotifyLimitCount = 0
				}
				if variant == "ssrf_reject" {
					system_setting.GetFetchSetting().EnableSSRFProtection = true
					settings.WebhookUrl = "http://127.0.0.1/hook"
					settings.BarkUrl = "http://127.0.0.1/{{title}}"
					settings.GotifyUrl = "http://127.0.0.1"
				}
			}
			if strings.HasPrefix(name, "gotify_priority_") {
				settings.NotifyType = "gotify"
				settings.GotifyPriority, _ = strconv.Atoi(strings.TrimPrefix(name, "gotify_priority_"))
			}
			switch name {
			case "webhook_values_nil":
				notification.Values = nil
			case "webhook_values_empty":
				notification.Values = []any{}
			case "webhook_fmt_legacy":
				notification.Content = "quota {{value}} {{value}}"
			}
			capture(sendFixtureNotification(7, "", settings, notification))
		}
		for _, request := range transport.Requests {
			if signature := request.Header.Get("X-Webhook-Signature"); signature != "" {
				mac := hmac.New(sha256.New, []byte(settings.WebhookSecret))
				mac.Write([]byte(request.Body))
				assert.Equal(t, hex.EncodeToString(mac.Sum(nil)), signature)
			}
		}
		memory := map[string]any{}
		notificationadapter.NotifyLimitStore.Range(func(key, value any) bool {
			v := value.(notificationadapter.LimitCount)
			memory[key.(string)] = map[string]any{"count": v.Count, "timestamp": v.Timestamp.Unix()}
			return true
		})
		extra["outcomes"], extra["memory"], extra["outbound"], extra["logs"] = outcomes, memory, transport.Requests, logs.String()
		extra["users"] = tf.Rows(t, model.DB, "users", "id,email,role,status,setting", "id")
		extra["channels"] = tf.Rows(t, model.DB, "channels", "id,status,models,other", "id")
		result = extra
		if name == "memory_cleanup_loop" {
			if t.Failed() {
				os.Exit(1)
			}
			f.Compare(t, "../../../service/testdata/notifications", name, result)
			os.Exit(0)
		}
	})
	return result
}

// The hook supplies replies before dial and preserves the actual GET/SET/TTL/pipeline command sequence.
type notificationRedis struct {
	variant    string
	values     map[string]string
	expiration map[string]time.Duration
	calls      [][]string
}

var notificationRedisReady = errors.New("notification fixture reply")

func (h *notificationRedis) BeforeProcess(ctx context.Context, command redis.Cmder) (context.Context, error) {
	h.record(command)
	return ctx, notificationRedisReady
}
func (h *notificationRedis) AfterProcess(_ context.Context, command redis.Cmder) error {
	command.SetErr(nil)
	key := ""
	if len(command.Args()) > 1 {
		key = fmt.Sprint(command.Args()[1])
	}
	switch command.Name() {
	case "get":
		if h.variant == "get_error" {
			command.SetErr(errors.New("fixture Redis GET failure"))
		} else if value, ok := h.values[key]; ok {
			command.(*redis.StringCmd).SetVal(value)
		} else {
			command.SetErr(redis.Nil)
		}
	case "set":
		if h.variant == "set_error" {
			command.SetErr(errors.New("fixture Redis SET failure"))
		} else {
			h.values[key] = fmt.Sprint(command.Args()[2])
			h.expiration[key] = 10 * time.Minute
			command.(*redis.StatusCmd).SetVal("OK")
		}
	case "ttl":
		if h.variant == "ttl_error" {
			command.SetErr(errors.New("fixture Redis TTL failure"))
		} else {
			command.(*redis.DurationCmd).SetVal(h.expiration[key])
		}
	default:
		command.SetErr(fmt.Errorf("undeclared notification Redis command %s", command.Name()))
	}
	return nil
}
func (h *notificationRedis) record(command redis.Cmder) {
	args := make([]string, len(command.Args()))
	for i, value := range command.Args() {
		args[i] = fmt.Sprint(value)
	}
	h.calls = append(h.calls, args)
}
func (h *notificationRedis) BeforeProcessPipeline(ctx context.Context, commands []redis.Cmder) (context.Context, error) {
	for _, command := range commands {
		h.record(command)
	}
	return ctx, notificationRedisReady
}
func (h *notificationRedis) AfterProcessPipeline(_ context.Context, commands []redis.Cmder) error {
	for _, command := range commands {
		command.SetErr(nil)
		key := ""
		if len(command.Args()) > 1 {
			key = fmt.Sprint(command.Args()[1])
		}
		if h.variant == "pipeline_error" {
			command.SetErr(errors.New("fixture Redis pipeline failure"))
			continue
		}
		switch command.Name() {
		case "multi":
			command.(*redis.StatusCmd).SetVal("OK")
		case "exec":
			command.(*redis.SliceCmd).SetVal([]any{int64(2), true})
		case "incrby":
			n, _ := strconv.Atoi(h.values[key])
			n++
			h.values[key] = strconv.Itoa(n)
			command.(*redis.IntCmd).SetVal(int64(n))
		case "expire":
			command.(*redis.BoolCmd).SetVal(true)
		default:
			command.SetErr(errors.New("undeclared pipeline command"))
		}
	}
	return nil
}
func seedNotificationRecipients(t *testing.T, settings dto.UserSetting) {
	t.Helper()
	for _, value := range []struct {
		id, role, status int
		watch            bool
		path, language   string
	}{{1, 1, 1, true, "user", "ru"}, {2, 10, 1, true, "admin", "ru"}, {3, 100, 1, true,
		"root", "zh-CN"}, {4, 10, 2, true, "disabled", "en"}, {5, 10, 1, false, "optout",
		"en"}} {
		s := settings
		s.WebhookUrl = "https://notify.invalid/" + value.path
		s.UpstreamModelUpdateNotifyEnabled = value.watch
		s.Language = value.language
		raw, err := common.Marshal(s)
		require.NoError(t, err)
		user := model.User{Id: value.id, Username: "notify-" + value.path, Email: value.path + "@example.invalid",
			Role: value.role, Status: value.status, AffCode: "notify-" + value.path, Setting: string(raw)}
		require.NoError(t, model.DB.Create(&user).Error)
	}
}
func notificationSMTP(t *testing.T) (string, <-chan string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { listener.Close() })
	messages := make(chan string, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		reader := textproto.NewReader(bufio.NewReader(connection))
		writer := bufio.NewWriter(connection)
		fmt.Fprint(writer, "220 fixture ESMTP\r\n")
		writer.Flush()
		var recipients []string
		for {
			line, err := reader.ReadLine()
			if err != nil {
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
				fmt.Fprint(writer, "250 fixture\r\n")
			case strings.HasPrefix(line, "MAIL"):
				fmt.Fprint(writer, "250 sender\r\n")
			case strings.HasPrefix(line, "RCPT"):
				recipients = append(recipients, line)
				fmt.Fprint(writer, "250 recipient\r\n")
			case line == "DATA":
				fmt.Fprint(writer, "354 send data\r\n")
				writer.Flush()
				var raw strings.Builder
				for {
					line, err := reader.R.ReadString('\n')
					if err != nil {
						return
					}
					if line == ".\r\n" {
						break
					}
					raw.WriteString(line)
				}
				messages <- strings.Join(recipients, "\r\n") + "\r\n" + raw.String()
				fmt.Fprint(writer, "250 queued\r\n")
			case line == "QUIT":
				fmt.Fprint(writer, "221 bye\r\n")
				writer.Flush()
				return
			default:
				fmt.Fprint(writer, "500 unsupported\r\n")
			}
			writer.Flush()
		}
	}()
	return listener.Addr().String(), messages
}

func sendFixtureNotification(id int, email string, settings dto.UserSetting, notice dto.Notify) error {
	return notifications.Send(id, email, notificationadapter.Preferences(settings), notificationadapter.Notice(notice))
}
