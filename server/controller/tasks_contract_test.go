package controller

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/internal/adapter/http/taskmedia"
	fixture "github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
	tasktransport "github.com/QuantumNous/new-api/internal/tasks/transport"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
)

var mjTickManifest = flag.Bool("tasks-mj-manifest", false, "list Midjourney tick cases")
var mjTickPreflight = flag.Bool("tasks-mj-preflight", false, "assert Midjourney ticks without goldens")

func init() { fixture.RegisterUpdate(); fixture.RejectCandidateUpdate() }
func mjTickCases() []string {
	result := []string{"queued",
		"in_progress",
		"success",
		"failure",
		"timeout",
		"timeout_equal",
		"null_id",
		"missing_channel",
		"unknown_id",
		"unchanged",
		"properties_only",
		"nil_properties",
		"nil_videos",
		"empty_videos",
		"cas_loss",
		"http_error",
		"transport_error",
		"bad_json",
		"no_token",
		"missing_token",
		"unbilled"}
	for _, family := range []string{"async", "mj"} {
		for _, state := range []string{"idle", "disabled", "active", "cancelled", "lease_lost"} {
			result = append(result, "schedule_"+family+"_"+state)
		}
	}
	for _, variant := range []string{"post",
		"invalid_method",
		"large_body",
		"bad_header",
		"credentialless_headers",
		"credentialless_body",
		"nil_descriptor",
		"empty_url",
		"userinfo",
		"fragment",
		"timeout",
		"redirect_same",
		"redirect_cross_auth",
		"redirect_cross_public",
		"redirect_self",
		"redirect_limit"} {
		result = append(result, "proxy_"+variant)
	}
	return result
}

func TestMidjourneyTickContract(t *testing.T) {
	updating := flag.Lookup("update").Value.String() == "true"
	if updating {
		fixture.CheckRecording(t)
	}
	if *mjTickManifest {
		data, err := common.Marshal(mjTickCases())
		require.NoError(t, err)
		fmt.Println("TASKS_MJ_MANIFEST " + string(data))
		return
	}
	if name := os.Getenv("TASKS_MJ_CASE"); name != "" {
		runMJTickContract(t, name, updating)
		return
	}
	for _, name := range mjTickCases() {
		t.Run(name, func(t *testing.T) {
			exe, err := os.Executable()
			require.NoError(t, err)
			cmd := exec.CommandContext(t.Context(),
				exe,
				"-test.run=^TestMidjourneyTickContract$",
				"-test.count=1",
				"-test.timeout=60s",
				"-update="+strconv.FormatBool(updating),
				"-tasks-mj-preflight="+strconv.FormatBool(*mjTickPreflight))
			cmd.Env = append(os.Environ(), "TASKS_MJ_CASE="+name, "TZ=UTC")
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", output)
			for _, line := range strings.Split(string(output), "\n") {
				if strings.HasPrefix(line, "TASKS_SNAPSHOT ") {
					fmt.Println(line)
				}
			}
		})
	}
}

type mjContractTransport struct {
	calls    []map[string]any
	name     string
	t        *testing.T
	response dto.MidjourneyDto
}

func (p *mjContractTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.String() != "https://203.0.113.10/mj/task/list-by-condition" {
		return nil, fmt.Errorf("undeclared MJ destination")
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	assert.Equal(p.t, "POST", r.Method)
	assert.Equal(p.t, "fixture-mj-key", r.Header.Get("mj-api-secret"))
	assert.JSONEq(p.t, `{"ids":["mj_policy"]}`, string(body))
	p.calls = append(p.calls, map[string]any{"method": r.Method, "url": r.URL.String(), "headers": r.Header.Clone(), "body": string(body)})
	if p.name == "transport_error" {
		return nil, errors.New("fixture MJ transport failure")
	}
	if p.name == "cas_loss" {
		if err := model.DB.Model(&model.Midjourney{}).Where("id=91").Updates(map[string]any{"status": "SUCCESS", "progress": "100%"}).Error; err != nil {
			return nil, err
		}
	}
	raw, err := common.Marshal([]dto.MidjourneyDto{p.response})
	if err != nil {
		return nil, err
	}
	status := 200
	if p.name == "http_error" {
		status = 503
	}
	if p.name == "bad_json" {
		raw = []byte(`{`)
	}
	return &http.Response{StatusCode: status, Request: r, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
}

func runMJTickContract(t *testing.T, name string, updating bool) {
	if strings.HasPrefix(name, "schedule_") {
		runTaskScheduleContract(t, name, updating)
		return
	}
	if strings.HasPrefix(name, "proxy_") {
		runTaskProxyContract(t, name, updating)
		return
	}
	service.InitHttpClient()
	synctest.Test(t, func(t *testing.T) {
		db := fixture.Open(t)
		nowMS := time.Now().UnixMilli()
		require.NoError(t, db.Create(&model.User{Id: 1, Username: "mj-user", Quota: 9950, UsedQuota: 50, RequestCount: 1, Group: "default", Status: 1}).Error)
		require.NoError(t,
			db.Create(&model.Token{Id: 11,
				UserId:      1,
				Key:         "mjpolicykey",
				Name:        "mj-key",
				Status:      1,
				ExpiredTime: -1,
				RemainQuota: 9950,
				UsedQuota:   50}).Error)
		baseURL := "https://203.0.113.10"
		require.NoError(t,
			db.Create(&model.Channel{Id: 71,
				Type:      constant.ChannelTypeMidjourney,
				Name:      "mj-provider",
				Key:       "fixture-mj-key",
				BaseURL:   &baseURL,
				Status:    1,
				UsedQuota: 50}).Error)
		task := &model.Midjourney{Id: 91, MjId: "mj_policy", UserId: 1, ChannelId: 71, BillingChannelId: 71, TokenId: 11, Action: "IMAGINE", Quota: 50, Code: 1,
			SubmitTime: nowMS,
			Progress:   "50%",
			Status:     "IN_PROGRESS",
			Properties: `{"keep":true}`,
			Buttons:    `[{"label":"keep"}]`,
			VideoUrls:  `[{"url":"https://203.0.113.10/old-video"}]`}
		response := dto.MidjourneyDto{MjId: task.MjId,
			Status:     task.Status,
			Progress:   task.Progress,
			SubmitTime: nowMS,
			VideoUrls:  []dto.ImgUrls{{Url: "https://203.0.113.10/old-video"}}}
		switch name {
		case "queued":
			response.Status = "SUBMITTED"
			response.Progress = "0%"
		case "in_progress":
			response.Progress = "70%"
		case "success":
			response.Status = "SUCCESS"
			response.Progress = "100%"
			response.FinishTime = nowMS
		case "failure", "no_token", "missing_token", "unbilled", "cas_loss":
			response.Status = "FAILURE"
			response.Progress = "100%"
			response.FailReason = "fixture provider failure"
		case "timeout":
			task.SubmitTime = nowMS - 3600001
		case "timeout_equal":
			task.SubmitTime = nowMS - 3600000
		case "null_id":
			task.MjId = ""
		case "missing_channel":
			task.ChannelId = 99
		case "unknown_id":
			response.MjId = "unknown"
		case "properties_only":
			task.VideoUrls = ""
			response.VideoUrls = nil
			response.Properties = &dto.Properties{}
		case "nil_properties":
			response.Progress = "70%"
			response.Properties = nil
			response.Buttons = nil
		case "nil_videos":
			response.Progress = "70%"
			response.VideoUrls = nil
		case "empty_videos":
			response.Progress = "70%"
			response.VideoUrls = []dto.ImgUrls{}
		}
		if name == "no_token" {
			task.TokenId = 0
		}
		if name == "missing_token" {
			require.NoError(t, db.Delete(&model.Token{}, 11).Error)
		}
		if name == "unbilled" {
			task.Quota = 0
		}
		require.NoError(t, db.Create(task).Error)
		transport := &mjContractTransport{calls: []map[string]any{}, name: name, t: t, response: response}
		service.GetHttpClient().Transport = transport
		events := []any{}
		for range 2 {
			summary := runMidjourneyTaskUpdateOnce(t.Context(), nil)
			events = append(events, map[string]any{"summary": summary, "state": fixture.State(t), "outbound": append([]map[string]any{}, transport.calls...)})
		}
		var saved model.Midjourney
		require.NoError(t, db.First(&saved, 91).Error)
		refunded := name == "failure" || name == "timeout" || name == "no_token" || name == "missing_token"
		if refunded {
			assert.Zero(t, saved.Quota)
			var user model.User
			require.NoError(t, db.First(&user, 1).Error)
			assert.Equal(t, 10000, user.Quota)
		} else if name != "unbilled" {
			assert.Equal(t, 50, saved.Quota)
		}
		fixture.Compare(t, "testdata/tasks-midjourney", name, fixture.StableRequestIDs(t, events), *mjTickPreflight, updating)
	})
}

// Actual scheduled handlers are invoked directly; no legacy loop or waiting ticker is introduced.
func runTaskScheduleContract(t *testing.T, name string, updating bool) {
	synctest.Test(t, func(t *testing.T) {
		db := fixture.Open(t)
		parts := strings.Split(name, "_")
		family, variant := parts[1], strings.Join(parts[2:], "_")
		var handler service.ScheduledSystemTaskHandler = asyncTaskPollHandler{}
		if family == "mj" {
			handler = midjourneyPollHandler{}
		}
		constant.UpdateTask = true
		require.False(t, handler.Enabled())
		require.Empty(t, fixture.Rows(t, db, "system_tasks", "*", "id"))
		if variant == "active" || variant == "disabled" {
			if family == "mj" {
				require.NoError(t, db.Create(&model.Midjourney{Id: 91, MjId: "schedule-mj", Status: "IN_PROGRESS", Progress: "50%", ChannelId: 99}).Error)
			} else {
				require.NoError(t, db.Create(&model.Task{ID: 81, TaskID: "schedule-async", Status: model.TaskStatusInProgress, ChannelId: 99, Platform: "kling"}).Error)
			}
		}
		if variant == "disabled" {
			constant.UpdateTask = false
		}
		require.Equal(t, variant == "active", handler.Enabled())
		require.Equal(t, 15*time.Second, handler.Interval())
		require.Nil(t, handler.NewPayload())
		task := &model.SystemTask{ID: 1, TaskID: "systask_fixture", Type: handler.Type(), Status: model.SystemTaskStatusRunning, LockedBy: "fixture-runner"}
		require.NoError(t, db.Create(task).Error)
		until := time.Now().Unix() + 60
		if variant == "lease_lost" {
			until = time.Now().Unix() - 1
		}
		require.NoError(t, db.Create(&model.SystemTaskLock{Type: handler.Type(), TaskID: task.TaskID, LockedBy: task.LockedBy, LockedUntil: until}).Error)
		ctx := t.Context()
		if variant == "cancelled" {
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			ctx = cancelled
		}
		handler.Run(ctx, task, task.LockedBy)
		var saved model.SystemTask
		require.NoError(t, db.First(&saved, 1).Error)
		expected := model.SystemTaskStatusSucceeded
		if variant == "lease_lost" {
			expected = model.SystemTaskStatusRunning
		}
		require.Equal(t, expected, saved.Status)
		fixture.Compare(t,
			"testdata/tasks-midjourney",
			name,
			map[string]any{"enabled": handler.Enabled(),
				"interval": handler.Interval().String(),
				"state":    fixture.State(t),
				"system_tasks": fixture.Rows(t,
					db,
					"system_tasks",
					"*",
					"id"),
				"locks": fixture.Rows(t,
					db,
					"system_task_locks",
					"*",
					"type")},
			*mjTickPreflight,
			updating)
	})
}

type proxyContractTransport struct {
	name  string
	calls []map[string]any
}

func (p *proxyContractTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != "203.0.113.10" && r.URL.Host != "203.0.113.11" {
		return nil, errors.New("undeclared proxy destination")
	}
	var raw []byte
	var err error
	if r.Body != nil {
		raw, err = io.ReadAll(r.Body)
		_ = r.Body.Close()
	}
	if err != nil {
		return nil, err
	}
	p.calls = append(p.calls, map[string]any{"method": r.Method, "url": r.URL.String(), "body": string(raw), "headers": r.Header.Clone()})
	if p.name == "timeout" {
		return nil, context.DeadlineExceeded
	}
	headers := http.Header{"Content-Type": {"video/mp4"}, "ETag": {"fixture-etag"}}
	status := 200
	if strings.HasPrefix(p.name, "redirect_") && (len(p.calls) == 1 || p.name == "redirect_limit") {
		status = 302
		target := "https://203.0.113.10/result"
		switch p.name {
		case "redirect_cross_auth", "redirect_cross_public":
			target = "https://203.0.113.11/result"
		case "redirect_self":
			target = "http://gateway.test/v1/videos/task_proxy/content"
		case "redirect_limit":
			target = "https://203.0.113.10/loop"
		}
		headers.Set("Location", target)
	}
	return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader("fixture-media")), Request: r}, nil
}
func runTaskProxyContract(t *testing.T, name string, updating bool) {
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"fetch_setting.enable_ssrf_protection": "false"}))
	service.InitHttpClient()
	synctest.Test(t, func(t *testing.T) {
		db := fixture.Open(t)
		variant := strings.TrimPrefix(name, "proxy_")
		base := "https://203.0.113.10"
		require.NoError(t, db.Create(&model.Channel{Id: 71, Key: "fixture-key", BaseURL: &base, Status: 1}).Error)
		task := &model.Task{ID: 81, TaskID: "task_proxy", ChannelId: 71, UserId: 1, Status: model.TaskStatusSuccess, Platform: "kling"}
		require.NoError(t, db.Create(task).Error)
		descriptor := &domain.MediaRequest{URL: base + "/source", Headers: map[string]string{"Authorization": "Bearer fixture-key", "Range": "bytes=0-1"}}
		expected, calls := 200, 1
		switch variant {
		case "post":
			descriptor.Method = "POST"
			descriptor.Body = []byte("fixture-body")
		case "invalid_method":
			descriptor.Method = "DELETE"
			expected, calls = 502, 0
		case "large_body":
			descriptor.Body = make([]byte, (1<<20)+1)
			expected, calls = 502, 0
		case "bad_header":
			descriptor.Headers["Connection"] = "close"
			expected, calls = 502, 0
		case "credentialless_headers":
			descriptor.Credentialless = true
			expected, calls = 502, 0
		case "credentialless_body":
			descriptor.Credentialless = true
			descriptor.Headers = nil
			descriptor.Body = []byte{}
			expected, calls = 502, 0
		case "nil_descriptor":
			descriptor = nil
			expected, calls = 500, 0
		case "empty_url":
			descriptor.URL = ""
			expected, calls = 410, 0
		case "userinfo":
			descriptor.URL = "https://user:pass@203.0.113.10/result"
			expected, calls = 502, 0
		case "fragment":
			descriptor.URL = base + "/result#part"
			expected, calls = 502, 0
		case "timeout":
			expected = 504
		case "redirect_same":
			calls = 2
		case "redirect_cross_auth", "redirect_self":
			expected = 502
		case "redirect_cross_public":
			descriptor.Credentialless = true
			descriptor.Headers = nil
			calls = 2
		case "redirect_limit":
			expected, calls = 502, 10
		}
		transport := &proxyContractTransport{name: variant, calls: []map[string]any{}}
		service.GetSSRFProtectedHTTPClient().Transport = transport
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest("GET", "http://gateway.test/v1/videos/task_proxy/content", nil)
		c.Request.Header.Set("Range", "bytes=2-5")
		c.Request.Header.Set("If-None-Match", "client-etag")
		err := taskmedia.NewProxy().Serve(c, domain.MediaTask{TaskID: task.TaskID, ChannelID: task.ChannelId}, descriptor)
		if err != nil {
			tasktransport.WriteMediaError(c, middleware.IsTaskArtifactAccess(c), err, func(err error) { logger.LogError(c.Request.Context(), err.Error()) })
		}
		require.Equal(t, expected, recorder.Code)
		require.Len(t, transport.calls, calls)
		if variant == "redirect_cross_public" {
			require.Empty(t, transport.calls[1]["headers"].(http.Header).Get("Authorization"))
		}
		if calls > 0 {
			require.Equal(t, "bytes=2-5", transport.calls[0]["headers"].(http.Header).Get("Range"))
		}
		fixture.Compare(t,
			"testdata/tasks-midjourney",
			name,
			map[string]any{"status": recorder.Code,
				"headers":  recorder.Header(),
				"body":     recorder.Body.String(),
				"outbound": transport.calls,
				"state":    fixture.State(t)},
			*mjTickPreflight,
			updating)
	})
}
