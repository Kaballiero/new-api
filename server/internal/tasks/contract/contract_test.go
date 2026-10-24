// Package contract pins async-task HTTP, storage and provider contracts before migration.
package contract

import (
	"bytes"
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskruntime "github.com/QuantumNous/new-api/internal/app/taskruntime"
	tasksapp "github.com/QuantumNous/new-api/internal/app/tasks"
	"github.com/QuantumNous/new-api/internal/contracttest"
	fixture "github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
	"github.com/QuantumNous/new-api/internal/contracttest/tasklog"
	"github.com/QuantumNous/new-api/internal/contracttest/taskmultipart"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/plugins"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/taskrecord"
	"github.com/QuantumNous/new-api/relay/taskregistry"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"
)

var tasksManifest = flag.Bool("tasks-manifest", false, "list task cases without fixtures")
var tasksPreflight = flag.Bool("tasks-preflight", false, "assert contracts without writing or reading goldens")
var tasksOriginCheck = flag.Bool("tasks-origin-check", false, "validate origin boundary without fixtures")

func init() { fixture.RejectCandidateUpdate() }

type taskCase struct {
	Name, Kind, Actor, Method, Path, Body, Variant, Plugin, Model, Response string
	Status, Calls, NewTasks                                                 int
	Params                                                                  map[string]string
}

func taskCases() []taskCase {
	cases := []taskCase{}
	for _, route := range []struct{ Name, Path, Role string }{
		{"own_tasks", "/api/task/self", "user"}, {"admin_tasks", "/api/task", "admin"},
		{"own_mj", "/api/mj/self", "user"}, {"admin_mj", "/api/mj/", "admin"},
		{"dashboard_artifacts", "/api/task/task_own/artifacts", "user"},
	} {
		for _, actor := range []string{"anonymous", "user", "admin", "root", "user_pat", "admin_pat", "root_pat", "api_key"} {
			status := 200
			if actor == "anonymous" || actor == "api_key" {
				status = 401
			} else if route.Role == "admin" && strings.HasPrefix(actor, "user") {
				status = 403
			}
			cases = append(cases, taskCase{Name: "access_" + route.Name + "_" + actor, Kind: "read", Actor: actor, Method: "GET", Path: route.Path, Status: status})
		}
	}
	queries := []string{"platform=kling", "platform=missing", "task_id=task_own", "status=SUCCESS", "action=text_to_video",
		"start_timestamp=101", "end_timestamp=100", "start_timestamp=bad", "channel_id=72", "user_id=2&user_ids=2",
		"platform=kling&status=SUCCESS", "p=2&page_size=1", "ps=1", "size=1", "page_size=101", "p=-1&page_size=-1"}
	for _, base := range []struct {
		Name,
		Path,
		Actor string
	}{{"own",
		"/api/task/self",
		"user"},
		{"admin",
			"/api/task",
			"admin"},
		{"mj_own",
			"/api/mj/self",
			"user"},
		{"mj_admin",
			"/api/mj/",
			"admin"}} {
		for i, query := range queries {
			cases = append(cases,
				taskCase{Name: fmt.Sprintf("filter_%s_%02d",
					base.Name,
					i),
					Kind:   "read",
					Actor:  base.Actor,
					Method: "GET",
					Path:   base.Path + "?" + query,
					Status: 200})
		}
		for _, variant := range []string{"empty", "read_error", "count_error", "forward"} {
			cases = append(cases,
				taskCase{Name: base.Name + "_" + variant,
					Kind:    "read",
					Actor:   base.Actor,
					Method:  "GET",
					Path:    base.Path,
					Variant: variant,
					Status:  200})
		}
	}
	for _, path := range []struct {
		Name,
		Path string
	}{{"detail",
		"/v1/tasks/task_own"},
		{"artifacts",
			"/v1/tasks/task_own/artifacts"},
		{"video",
			"/v1/videos/task_own/content"},
		{"content",
			"/v1/tasks/task_own/artifacts/video/content"}} {
		for _, variant := range []string{"own", "foreign", "missing", "anonymous", "dashboard_credential", "db_error"} {
			status := 200
			switch variant {
			case "foreign", "missing":
				status = 404
			case "anonymous", "dashboard_credential":
				status = 401
			case "db_error":
				status = 500
			}
			calls := 0
			if (path.Name == "video" || path.Name == "content") && variant == "own" {
				calls = 1
			}
			cases = append(cases,
				taskCase{Name: path.Name + "_" + variant,
					Kind:    "gateway_read",
					Actor:   "api_key",
					Method:  "GET",
					Path:    path.Path,
					Variant: variant,
					Status:  status,
					Calls:   calls})
		}
	}
	for _, variant := range []string{"valid",
		"tampered",
		"rotated",
		"foreign",
		"duplicate",
		"disabled_owner",
		"bearer_bad_access",
		"wrong_artifact",
		"no_expiry"} {
		status, calls := 404, 0
		if variant == "valid" || variant == "no_expiry" {
			status, calls = 200, 1
		}
		cases = append(cases,
			taskCase{Name: "capability_" + variant,
				Kind:    "capability",
				Actor:   "anonymous",
				Method:  "GET",
				Path:    "/v1/tasks/task_own/artifacts/video/content",
				Variant: variant,
				Status:  status,
				Calls:   calls})
	}
	for _, method := range []string{"GET", "HEAD"} {
		for _, status := range []int{200, 206, 304, 416, 401, 403, 404, 410, 429, 500} {
			expected := status
			switch status {
			case 401, 403, 500:
				expected = 502
			case 404, 410:
				expected = 410
			case 429:
				expected = 503
			}
			cases = append(cases,
				taskCase{Name: fmt.Sprintf("media_%s_%d",
					method,
					status),
					Kind:    "media",
					Actor:   "api_key",
					Method:  method,
					Path:    "/v1/videos/task_own/content",
					Variant: strconv.Itoa(status),
					Status:  expected,
					Calls:   1})
		}
		for _, variant := range []string{"data_url", "self_url", "invalid_url", "transport_error", "range"} {
			status, calls := 200, 0
			switch variant {
			case "self_url":
				status = 410
			case "invalid_url":
				status = 502
			case "transport_error":
				status, calls = 502, 1
			case "range":
				status, calls = 206, 1
			}
			cases = append(cases,
				taskCase{Name: "media_" + method + "_" + variant,
					Kind:    "media",
					Actor:   "api_key",
					Method:  method,
					Path:    "/v1/videos/task_own/content",
					Variant: variant,
					Status:  status,
					Calls:   calls})
		}
	}
	families := []taskCase{
		{Plugin: "vertex-ai", Model: "veo-3.0-generate-001", Response: `{"name":"projects/test-project/locations/us-central1/operations/upstream-fixture"}`},
		{Plugin: "openrouter-video", Model: "runway/gen-4.5", Response: `{"id":"upstream-fixture","status":"pending"}`},
		{Plugin: "sora", Model: "sora-2", Response: `{"id":"upstream-fixture","status":"queued"}`},
		{Plugin: "kling", Model: "kling-v1", Response: `{"code":0,"data":{"task_id":"upstream-fixture"}}`},
		{Plugin: "jimeng", Model: "jimeng_vgfm_t2v_l20", Response: `{"code":10000,"data":{"task_id":"upstream-fixture"}}`},
		{Plugin: "alibaba",
			Model:    "wan2.6-t2v",
			Response: `{"output":{"task_id":"upstream-fixture","task_status":"PENDING"}}`,
			Params:   map[string]string{"seconds": "5.00"}},

		{Plugin: "doubao", Model: "doubao-seedance-1-0-pro-250528", Response: `{"id":"upstream-fixture"}`},
		{Plugin: "hailuo", Model: "MiniMax-H3", Response: `{"task_id":"upstream-fixture"}`},
		{Plugin: "vidu", Model: "viduq2", Response: `{"task_id":"upstream-fixture"}`},
		{Plugin: "google", Model: "veo-3.0-generate-001", Response: `{"name":"operations/upstream-fixture"}`, Params: map[string]string{"seconds": "5.00"}},
		{Plugin: "sunoapi", Model: "suno_music", Response: `{"code":"success","message":"accepted","data":"upstream-fixture"}`},
	}
	for _, family := range families {
		for _, variant := range []string{"success", "upstream_error", "malformed_response", "insert_error", "quota_denied", "explicit_zero"} {
			tc := family
			tc.Name = "submit_" + family.Plugin + "_" + variant
			tc.Kind = "submit"
			tc.Actor = "api_key"
			tc.Method = "POST"
			tc.Path = "/v1/tasks/" + family.Plugin
			tc.Variant = variant
			tc.Body = fmt.Sprintf(`{"model":%q,"prompt":"fixture","duration":5}`, tc.Model)
			if tc.Plugin == "sunoapi" {
				tc.Path = "/suno/submit/music"
			}
			if tc.Plugin == "vertex-ai" {
				tc.Body = fmt.Sprintf(`{"model":%q,"prompt":"fixture","duration":8}`, tc.Model)
			}
			if tc.Plugin == "openrouter-video" {
				tc.Calls = 2
			}
			tc.Status, tc.Calls, tc.NewTasks = 200, 1, 1
			if tc.Plugin == "vertex-ai" || tc.Plugin == "openrouter-video" {
				tc.Calls = 2
			}
			switch variant {
			case "upstream_error", "malformed_response", "insert_error":
				tc.Status, tc.NewTasks = 500, 0
			case "quota_denied":
				tc.Status, tc.Calls, tc.NewTasks = 403, 0, 0
			case "explicit_zero":
				tc.Body = fmt.Sprintf(`{"model":%q,"prompt":"fixture","duration":0}`, tc.Model)
			}
			if variant == "malformed_response" {
				tc.Status = 502
			}
			if variant == "quota_denied" && (tc.Plugin == "vertex-ai" || tc.Plugin == "openrouter-video") {
				tc.Calls = 1
			}
			if variant == "explicit_zero" {
				if tc.Plugin == "openrouter-video" || tc.Plugin == "alibaba" || tc.Plugin == "hailuo" {
					tc.Status, tc.Calls, tc.NewTasks = 400, 0, 0
				}
				if tc.Plugin == "google" {
					tc.Params = map[string]string{"seconds": "8.00"}
				}
			}
			cases = append(cases, tc)
		}
	}
	for _, native := range []struct{ Plugin, Path, Body string }{
		{"sora", "/v1/videos", `{"model":"sora-2","prompt":"fixture","seconds":"4"}`},
		{"sora", "/v1/video/generations", `{"model":"sora-2","prompt":"fixture","seconds":"4"}`},
		{"kling", "/kling/v1/videos/text2video", `{"model_name":"kling-v1","prompt":"fixture","duration":"5"}`},
		{"doubao",
			"/doubao/api/v3/contents/generations/tasks",
			`{"model":"doubao-seedance-1-0-pro-250528","content":[{"type":"text","text":"fixture"}],"duration":5}`},

		{"alibaba", "/ali/api/v1/services/aigc/video-generation/video-synthesis", `{"model":"wan2.6-t2v","input":{"prompt":"fixture"},"parameters":{"duration":5}}`},
		{"jimeng", "/jimeng/?Action=CVSync2AsyncSubmitTask&Version=2022-08-31", `{"req_key":"jimeng_vgfm_t2v_l20","prompt":"fixture","frames":121}`},
		{"sunoapi", "/suno/submit/lyrics", `{"prompt":"fixture lyrics"}`},
	} {
		for _, family := range families {
			if family.Plugin == native.Plugin {
				tc := family
				routePath, _, _ := strings.Cut(native.Path, "?")
				tc.Name = "native_" + tc.Plugin + "_" + strings.ReplaceAll(strings.Trim(routePath, "/"), "/", "_")
				tc.Kind = "submit"
				tc.Actor = "api_key"
				tc.Method = "POST"
				tc.Path = native.Path
				tc.Body = native.Body
				tc.Variant = "success"
				tc.Status, tc.Calls, tc.NewTasks = 200, 1, 1
				if native.Plugin == "sunoapi" {
					tc.Model = "suno_lyrics"
				}
				if native.Plugin == "jimeng" {
					tc.Status, tc.Calls, tc.NewTasks = 307, 0, 0
				}
				cases = append(cases, tc)
			}
		}
	}
	for _, variant := range []string{"ratios", "usage"} {
		params := map[string]string{"seconds": "5.00", "resolution": "2.00"}
		if variant == "usage" {
			params = map[string]string{"seconds": "6", "resolution": "768P", "input_images": "0", "input_video_seconds": "0"}
		}
		cases = append(cases, taskCase{Name: "billing_content_" + variant, Kind: "record", Variant: variant, Status: 200, Params: params})
	}
	for _, family := range families {
		tc := family
		tc.Kind = "provider_poll"
		tc.Name = "provider_poll_" + tc.Plugin
		tc.Status, tc.Calls = 200, 1
		switch tc.Plugin {
		case "sora":
			tc.Response = `{"id":"upstream-fixture","status":"completed"}`
		case "kling":
			tc.Response = `{"code":0,"data":{"task_id":"upstream-fixture","task_status":"succeed"}}`
		case "jimeng":
			tc.Response = `{"code":10000,"data":{"status":"done","video_url":"https://203.0.113.10/video"}}`
		case "alibaba":
			tc.Response = `{"output":{"task_status":"SUCCEEDED","video_url":"https://203.0.113.10/video"}}`
		case "doubao":
			tc.Response = `{"status":"succeeded","content":{"video_url":"https://203.0.113.10/video"}}`
		case "hailuo":
			tc.Response = `{"task":{"task_id":"upstream-fixture","status":"succeeded","content":{"url":"https://203.0.113.10/video"}}}`
		case "vidu":
			tc.Response = `{"state":"success","creations":[{"url":"https://203.0.113.10/video"}]}`
		case "google":
			tc.Response = `{"name":"operations/upstream-fixture","done":true,"response":{"generateVideoResponse":{"generatedSamples":[{"video":{"uri":"https://203.0.113.10/video"}}]}}}`
		case "vertex-ai":
			tc.Calls = 2
			tc.Response = `{"name":"projects/test-project/locations/us-central1/publishers/google/models/veo-3.0-generate-001/operations/upstream-fixture","done":true,"response":{"videos":[{"gcsUri":"gs://fixture/video"}]}}`
		case "sunoapi":
			tc.Response = `{"code":"success","data":[{"task_id":"upstream-fixture","status":"SUCCESS","action":"MUSIC","submit_time":100,"start_time":101,"finish_time":102,"data":[]}]}`
		case "openrouter-video":
			tc.Response = `{"id":"upstream-fixture","status":"completed"}`
		}
		cases = append(cases, tc)
	}
	cases = append(cases,
		taskCase{Name: "submit_kling_jwt",
			Kind:     "submit",
			Actor:    "api_key",
			Method:   "POST",
			Path:     "/v1/tasks/kling",
			Plugin:   "kling",
			Model:    "kling-v1",
			Body:     `{"model":"kling-v1","prompt":"fixture","duration":5}`,
			Response: `{"code":0,"data":{"task_id":"upstream-fixture"}}`,
			Variant:  "jwt",
			Status:   200,
			Calls:    1,
			NewTasks: 1})
	cases = append(cases,
		taskCase{Name: "submit_kling_subscription",
			Kind:     "submit",
			Actor:    "api_key",
			Method:   "POST",
			Path:     "/v1/tasks/kling",
			Plugin:   "kling",
			Model:    "kling-v1",
			Body:     `{"model":"kling-v1","prompt":"fixture","duration":5}`,
			Response: `{"code":0,"data":{"task_id":"upstream-fixture"}}`,
			Variant:  "subscription",
			Status:   200,
			Calls:    1,
			NewTasks: 1})
	cases = append(cases,
		taskCase{Name: "submit_sora_multipart",
			Kind:     "submit",
			Actor:    "api_key",
			Method:   "POST",
			Path:     "/v1/videos",
			Plugin:   "sora",
			Model:    "sora-2",
			Response: `{"id":"upstream-fixture","status":"queued"}`,
			Variant:  "multipart",
			Status:   200,
			Calls:    1,
			NewTasks: 1})
	for _, variant := range []string{"valid", "invalid_key", "duplicate_key", "invalid_type", "invalid_mime", "too_many", "unavailable"} {
		status := 500
		if variant == "valid" {
			status = 200
		}
		if variant == "unavailable" {
			status = 503
		}
		cases = append(cases,
			taskCase{Name: "plugin_artifacts_" + variant,
				Kind:    "plugin_artifacts",
				Actor:   "api_key",
				Method:  "GET",
				Path:    "/v1/tasks/task_own/artifacts",
				Variant: variant,
				Status:  status})
	}
	for _, profile := range []struct{ Name, Actor, Path, Variant string }{
		{"projection_own", "user", "/api/task/self", "provenance"}, {"projection_admin", "admin", "/api/task", "provenance"},
		{"projection_root", "root", "/api/task", "provenance"}, {"projection_root_self", "root", "/api/task/self", "provenance_self_root"},
		{"detail_legacy_url", "api_key", "/v1/tasks/task_own", "legacy_url"}, {"detail_created_fallback", "api_key", "/v1/tasks/task_own", "created_fallback"},
		{"detail_reconciliation",
			"api_key",
			"/v1/tasks/task_own",
			"reconciliation"},
		{"artifacts_reconciliation",
			"api_key",
			"/v1/tasks/task_own/artifacts",
			"reconciliation"},
	} {
		cases = append(cases,
			taskCase{Name: profile.Name,
				Kind:    "read",
				Actor:   profile.Actor,
				Method:  "GET",
				Path:    profile.Path,
				Variant: profile.Variant,
				Status:  200})
	}
	for _, variant := range []string{"get", "head", "missing_blob", "corrupt_blob", "invalid_checksum", "disabled_store"} {
		method, status := "GET", 200
		if variant == "head" {
			method = "HEAD"
		}
		if variant != "get" && variant != "head" {
			status = 503
		}
		cases = append(cases,
			taskCase{Name: "stored_artifact_" + variant,
				Kind:    "stored_artifact",
				Actor:   "api_key",
				Method:  method,
				Path:    "/v1/tasks/task_own/artifacts/result/content",
				Variant: variant,
				Status:  status})
	}
	for _, action := range []string{"imagine",
		"action",
		"change",
		"simple-change",
		"modal",
		"describe",
		"blend",
		"shorten",
		"edits",
		"video",
		"upload-discord-images"} {
		modelAction := strings.ToUpper(action)
		switch action {
		case "action", "change", "simple-change":
			modelAction = "UPSCALE"
		case "upload-discord-images":
			modelAction = "UPLOAD"
		}
		cases = append(cases,
			taskCase{Name: "mj_" + action,
				Kind:   "mj",
				Model:  "mj_" + strings.ToLower(modelAction),
				Actor:  "api_key",
				Method: "POST",
				Path:   "/mj/submit/" + action,

				Body:    `{"prompt":"fixture --fast","taskId":"mj_own","action":"UPSCALE","index":1,"customId":"MJ::JOB::upsample::1::mj_own","content":"mj_own u1","base64Array":["YQ==","Yg=="],"notifyHook":"https://discard.example.test","accountFilter":{"secret":"fixture"}}`,
				Variant: action,
				Status:  200,
				Calls:   1})
	}
	for _, variant := range []string{"fetch", "list", "seed", "image", "notify", "notify_missing", "notify_invalid"} {
		cases = append(cases,
			taskCase{Name: "mj_read_" + variant,
				Kind:    "mj_read",
				Actor:   "api_key",
				Method:  "GET",
				Path:    "/mj/task/mj_own/fetch",
				Variant: variant,
				Status:  200})
	}
	for _, variant := range []string{"code21", "code22", "code3", "code23", "swap", "inpaint", "custom_zoom", "upstream_error"} {
		tc := taskCase{Name: "mj_variant_" + variant,
			Kind:    "mj",
			Actor:   "api_key",
			Method:  "POST",
			Path:    "/mj/submit/imagine",
			Model:   "mj_imagine",
			Body:    `{"prompt":"fixture","taskId":"mj_own","customId":"MJ::JOB::Inpaint::1::mj_own"}`,
			Variant: variant,
			Status:  200,
			Calls:   1}
		switch variant {
		case "swap":
			tc.Path = "/mj/insight-face/swap"
			tc.Model = "swap_face"
			tc.Body = `{"sourceBase64":"YQ==","targetBase64":"Yg=="}`
		case "inpaint":
			tc.Path = "/mj/submit/action"
			tc.Model = "mj_inpaint"
		case "custom_zoom":
			tc.Path = "/mj/submit/action"
			tc.Model = "mj_custom_zoom"
			tc.Body = `{"taskId":"mj_own","customId":"MJ::JOB::CustomZoom::1::mj_own"}`
		case "upstream_error":
			tc.Status = 500
		}
		cases = append(cases, tc)
	}
	return cases
}

func TestTasksContract(t *testing.T) {
	updating := flag.Lookup("update").Value.String() == "true"
	if *tasksOriginCheck {
		fixture.CheckRecording(t)
		return
	}
	if updating {
		fixture.CheckRecording(t)
	}
	cases := taskCases()
	seen := map[string]bool{}
	for _, tc := range cases {
		require.False(t, seen[tc.Name], tc.Name)
		seen[tc.Name] = true
		require.NotZero(t, tc.Status)
	}
	if destination := os.Getenv("TASKS_CONTRACT_LOCALE_EVIDENCE"); destination != "" && os.Getenv("TASKS_CONTRACT_CASE") == "" {
		require.False(t, updating)
		writeTaskLocales(t, destination)
		return
	}
	if *tasksManifest {
		raw, err := common.Marshal(cases)
		require.NoError(t, err)
		fmt.Println("TASKS_MANIFEST " + string(raw))
		return
	}
	if selected := os.Getenv("TASKS_CONTRACT_CASE"); selected != "" {
		for _, tc := range cases {
			if tc.Name == selected {
				runTaskCase(t, tc, updating)
				return
			}
		}
		t.Fatalf("unknown task case %q", selected)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			exe, err := os.Executable()
			require.NoError(t, err)
			cmd := exec.CommandContext(t.Context(),
				exe,
				"-test.run=^TestTasksContract$",
				"-test.count=1",
				"-test.timeout=120s",
				"-update="+strconv.FormatBool(updating),
				"-tasks-preflight="+strconv.FormatBool(*tasksPreflight))
			cmd.Env = append(os.Environ(), "TASKS_CONTRACT_CASE="+tc.Name, "TZ=UTC")
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

type outgoing struct {
	Method, URL, Body string
	Headers           http.Header
	Host              string
	ContentLength     int64
	TransferEncoding  []string
}
type taskProvider struct {
	mu    sync.Mutex
	calls []outgoing
	tc    taskCase
}

func (p *taskProvider) RoundTrip(r *http.Request) (*http.Response, error) {
	var raw []byte
	var err error
	capturedHeaders := r.Header.Clone()
	if r.Body != nil {
		defer r.Body.Close()
	}
	if r.Body != nil {
		raw, err = io.ReadAll(r.Body)
	}
	if err != nil {
		return nil, err
	}
	if r.URL.Host == "www.googleapis.com" && r.URL.Path == "/oauth2/v4/token" {
		form, err := url.ParseQuery(string(raw))
		if err != nil {
			return nil, err
		}
		parts := strings.Split(form.Get("assertion"), ".")
		if len(parts) != 3 {
			return nil, errors.New("invalid fixture JWT")
		}
		block, _ := pem.Decode([]byte(fixture.PrivateKeyPEM))
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		signature, err := base64.RawURLEncoding.DecodeString(parts[2])
		if err != nil {
			return nil, err
		}
		claims, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			return nil, err
		}
		var values map[string]json.RawMessage
		if err := common.Unmarshal(claims, &values); err != nil {
			return nil, err
		}
		if len(values) != 5 {
			return nil, errors.New("unexpected OAuth JWT claim set")
		}
		var decoded struct {
			Aud, Iss, Scope string
			Iat, Exp        int64
		}
		if err := common.Unmarshal(claims, &decoded); err != nil {
			return nil, err
		}
		if decoded.Iss != "fixture@example.test" || decoded.Aud != "https://www.googleapis.com/oauth2/v4/token" || decoded.Scope != "https://www.googleapis.com/auth/cloud-platform" || decoded.Iat != time.Now().Unix() || decoded.Exp != time.Now().Add(35*time.Minute).Unix() {
			return nil, errors.New("unexpected OAuth JWT claims")
		}
		digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
		if err := rsa.VerifyPKCS1v15(&key.(*rsa.PrivateKey).PublicKey, crypto.SHA256, digest[:], signature); err != nil {
			return nil, err
		}
		p.mu.Lock()
		p.calls = append(p.calls, outgoing{r.Method, r.URL.String(), string(raw), capturedHeaders, r.Host, r.ContentLength, r.TransferEncoding})
		p.mu.Unlock()
		return &http.Response{StatusCode: 200,
				Request: r,
				Header:  http.Header{"Content-Type": {"application/json"}},
				Body:    io.NopCloser(strings.NewReader(`{"access_token":"fixture-oauth-token","expires_in":3600}`))},
			nil
	}
	if p.tc.Variant == "multipart" {
		if p.tc.Plugin != "sora" || r.Header.Get("Content-Type") != "" {
			return nil, errors.New("unexpected legacy Sora multipart request")
		}
		raw, err = taskmultipart.CanonicalBody(raw, []taskmultipart.Part{
			{Name: "model", Headers: "Content-Disposition: form-data; name=model", Data: []byte("sora-2")},
			{Name: "prompt", Headers: "Content-Disposition: form-data; name=prompt", Data: []byte("fixture")},
			{Name: "seconds", Headers: "Content-Disposition: form-data; name=seconds", Data: []byte("4")},
			{Name: "input_reference", Filename: "fixture.png", Headers: "Content-Disposition: form-data; filename=fixture.png; name=input_reference\r\nContent-Type: application/octet-stream", Data: []byte("fixture-image")},
		})
		if err != nil {
			return nil, err
		}
	}

	if r.URL.Host != "203.0.113.10" {
		return nil, fmt.Errorf("undeclared provider destination: %s", r.URL.Host)
	}
	if p.tc.Variant == "jwt" {
		parts := strings.Split(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), ".")
		if len(parts) != 3 {
			return nil, errors.New("invalid Kling JWT")
		}
		mac := hmac.New(sha256.New, []byte("fixture-secret"))
		_, _ = io.WriteString(mac, parts[0]+"."+parts[1])
		if base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) != parts[2] {
			return nil, errors.New("invalid Kling signature")
		}
		claims, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			return nil, err
		}
		var value struct {
			Iss      string `json:"iss"`
			Exp, Nbf int64
		}
		if err := common.Unmarshal(claims, &value); err != nil {
			return nil, err
		}
		if value.Iss != "fixture-access" || value.Exp != time.Now().Unix()+1800 || value.Nbf != time.Now().Unix()-5 {
			return nil, errors.New("invalid Kling claims")
		}
	}
	p.mu.Lock()
	p.calls = append(p.calls, outgoing{r.Method, r.URL.String(), string(raw), capturedHeaders, r.Host, r.ContentLength, r.TransferEncoding})
	p.mu.Unlock()
	status, body := 200, p.tc.Response
	if r.URL.Path == "/v1/videos/models" {
		return &http.Response{StatusCode: 200,
				Request: r,
				Header:  http.Header{"Content-Type": {"application/json"}},
				Body:    io.NopCloser(strings.NewReader(`{"data":[{"id":"runway/gen-4.5","supported_durations":[5,10],"pricing_skus":{"cents_per_second_output":"0.01"}}]}`))},
			nil
	}
	if p.tc.Kind == "media" || p.tc.Kind == "capability" || p.tc.Kind == "gateway_read" {
		body = "fixture-video"
		if n, err := strconv.Atoi(p.tc.Variant); err == nil {
			status = n
		}
		if p.tc.Variant == "range" {
			status, body = 206, "ideo"
		}
		if p.tc.Variant == "transport_error" {
			return nil, errors.New("fixture transport failure")
		}
	}
	if p.tc.Kind == "submit" {
		if p.tc.Variant == "upstream_error" {
			status, body = 500, `{"error":"fixture upstream failure"}`
		}
		if p.tc.Variant == "malformed_response" {
			body = `{}`
		}
	}
	if p.tc.Kind == "mj" {
		body = `{"code":1,"description":"accepted","result":"mj_submitted"}`
		switch p.tc.Variant {
		case "code21":
			body = `{"code":21,"description":"exists","result":"mj_submitted","properties":{"status":"SUCCESS","imageUrl":"https://203.0.113.10/image"}}`
		case "code22":
			body = `{"code":22,"description":"queued","result":"mj_submitted"}`
		case "code3":
			body = `{"code":3,"description":"no instance","result":""}`
		case "code23":
			body = `{"code":23,"description":"queue full","result":""}`
		case "upstream_error":
			status = 500
		}
	}
	if p.tc.Kind == "mj_read" {
		body = `{"code":1,"result":"fixture"}`
		if p.tc.Variant == "image" {
			body = "fixture-image"
		}
	}
	return &http.Response{StatusCode: status,
			Request: r,
			Header: http.Header{"Content-Type": {"video/mp4"},
				"Accept-Ranges": {"bytes"},
				"Etag":          {"fixture-etag"},
				"Content-Range": {"bytes 1-4/13"},
				"Retry-After":   {"17"},
				"X-Discard":     {"private"}},
			Body: io.NopCloser(strings.NewReader(body))},
		nil
}

type httpFixture struct {
	env        *contracttest.Env
	identities map[string]service.AuthIdentity
	bearers    map[string]string
	provider   *taskProvider
}

func newHTTPFixture(t *testing.T, tc taskCase) *httpFixture {
	t.Helper()
	fixture.InstallDBClock(t)
	env := contracttest.New(t)
	fixture.Configure()
	require.NoError(t,
		config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.billing_mode": `{}`,
			"billing_setting.billing_expr":          `{}`,
			"group_ratio_setting.group_ratio":       `{"default":1}`,
			"group_ratio_setting.group_group_ratio": `{}`,
			"fetch_setting.enable_ssrf_protection":  "false",
			"channel_affinity_setting.enabled":      "false"}))
	common.CryptoSecret = "tasks-contract-test-secret"
	common.RetryTimes = 0
	common.QuotaRemindThreshold = -1000000000
	common.SetPerformanceMonitorConfig(common.PerformanceMonitorConfig{})
	constant.TaskTimeoutMinutes, constant.TaskQueryLimit = 0, 100
	setting.ModelRequestRateLimitEnabled = false
	setting.MjForwardUrlEnabled = tc.Variant == "forward"
	system_setting.ServerAddress = "https://console.example.test"
	operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = 100
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeTokens
	require.NoError(t,
		env.DB.Create(&model.ModelCostFXRow{Source: "cbr-xml-daily.ru",
			EffectiveAt: fixture.Epoch,
			FetchedAt:   fixture.Epoch,
			Version:     1,
			RatesJSON:   `{"USD":100}`}).Error)
	require.NoError(t, model.LoadModelCostFX(context.Background(), "cbr-xml-daily.ru"))
	logDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "logs.db")+"?_pragma=synchronous(OFF)"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, logDB.AutoMigrate(&model.Log{}, &model.AuditLog{}))
	model.LOG_DB = logDB
	t.Cleanup(func() { pool, err := logDB.DB(); require.NoError(t, err); require.NoError(t, pool.Close()) })
	f := &httpFixture{env: env,
		identities: map[string]service.AuthIdentity{},
		bearers: map[string]string{"anonymous": "",
			"api_key":   "sk-tasksfixturekey",
			"user_pat":  env.UserPAT,
			"admin_pat": "tasks-admin-pat",
			"root_pat":  "tasks-root-pat"},
		provider: &taskProvider{tc: tc,
			calls: []outgoing{}}}
	for actor, token := range map[string]string{"user": env.User, "admin": env.Admin, "root": env.Root} {
		identity, err := service.ParseAccessToken(token)
		require.NoError(t, err)
		f.identities[actor] = identity
	}
	for id, pat := range map[int]string{2: "tasks-admin-pat", 3: "tasks-root-pat"} {
		require.NoError(t, env.DB.Model(&model.User{}).Where("id = ?", id).Update("access_token", pat).Error)
	}
	require.NoError(t,
		env.DB.Create(&model.Token{Id: 101,
			UserId:      1,
			Key:         "tasksfixturekey",
			Name:        "tasks-key",
			Status:      1,
			ExpiredTime: -1,
			RemainQuota: 1000000,
			Group:       "default"}).Error)
	require.NoError(t, env.DB.Model(&model.User{}).Where("id = ?", 1).Update("setting", `{"billing_preference":"wallet_only"}`).Error)
	baseURL := "https://203.0.113.10"
	channelType := constant.ChannelTypeKling
	if tc.Kind == "mj" || tc.Kind == "mj_read" {
		channelType = constant.ChannelTypeMidjourney
	}
	if tc.Plugin != "" {
		adaptor := taskregistry.GetTaskAdaptor(constant.TaskPlatform(tc.Plugin))
		require.NotNil(t, adaptor)
		channelType = constant.ChannelTypeTaskPlugin
	}
	channel := model.Channel{Id: 71,
		Type:    channelType,
		Key:     "sk-fixture-provider",
		Status:  common.ChannelStatusEnabled,
		Name:    "tasks-provider",
		BaseURL: &baseURL,
		Models:  tc.Model,
		Group:   "default",
		AutoBan: common.GetPointer(0)}
	if tc.Path == "/v1/video/generations" && tc.Plugin == "sora" {
		channel.Type = constant.ChannelTypeSora
	}
	if tc.Variant == "jwt" {
		channel.Key = "fixture-access|fixture-secret"
	}
	if tc.Plugin == "vertex-ai" {
		credentials,
			err := common.Marshal(map[string]string{"project_id": "test-project",
			"client_email": "fixture@example.test",
			"private_key":  fixture.PrivateKeyPEM})
		require.NoError(t, err)
		channel.Key = string(credentials)
	}
	if tc.Plugin != "" {
		channel.SetSetting(dto.ChannelSettings{TaskPluginKey: tc.Plugin})
	}
	require.NoError(t, env.DB.Create(&channel).Error)
	if tc.Model != "" {
		require.NoError(t, channel.AddAbilities(nil))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(fmt.Sprintf(`{%q:0.001}`, tc.Model)))
	}
	require.NoError(t,
		env.DB.Create(&model.Task{ID: 81,
			TaskID:      "task_own",
			CreatedAt:   fixture.Epoch,
			UpdatedAt:   fixture.Epoch,
			UserId:      1,
			ChannelId:   71,
			Platform:    "kling",
			Action:      constant.TaskActionTextToVideo,
			Status:      model.TaskStatusSuccess,
			Progress:    "100%",
			SubmitTime:  100,
			FinishTime:  101,
			PrivateData: model.TaskPrivateData{ResultURL: baseURL + "/video"}}).Error)
	require.NoError(t,
		env.DB.Create(&model.Task{ID: 82,
			TaskID:     "task_foreign",
			CreatedAt:  fixture.Epoch,
			UpdatedAt:  fixture.Epoch,
			UserId:     2,
			ChannelId:  72,
			Platform:   "suno",
			Status:     model.TaskStatusFailure,
			Progress:   "100%",
			SubmitTime: 101,
			FailReason: "provider failure"}).Error)
	require.NoError(t,
		env.DB.Create(&model.Midjourney{Id: 91,
			MjId:       "mj_own",
			UserId:     1,
			ChannelId:  71,
			Status:     "SUCCESS",
			Progress:   "100%",
			ImageUrl:   baseURL + "/image",
			SubmitTime: 100,
			FinishTime: 101,
			Quota:      0}).Error)
	require.NoError(t,
		env.DB.Create(&model.Midjourney{Id: 92,
			MjId:       "mj_foreign",
			UserId:     2,
			ChannelId:  72,
			Status:     "FAILURE",
			Progress:   "100%",
			SubmitTime: 101}).Error)
	if tc.Variant == "empty" {
		require.NoError(t, env.DB.Where("1=1").Delete(&model.Task{}).Error)
		require.NoError(t, env.DB.Where("1=1").Delete(&model.Midjourney{}).Error)
	}
	if tc.Variant == "subscription" {
		require.NoError(t,
			env.DB.Create(&model.SubscriptionPlan{Id: 31,
				Title:            "Task plan",
				Enabled:          true,
				DurationUnit:     "month",
				DurationValue:    1,
				TotalAmount:      10000,
				QuotaResetPeriod: "never"}).Error)
		require.NoError(t,
			env.DB.Create(&model.UserSubscription{Id: 21,
				UserId:      1,
				PlanId:      31,
				AmountTotal: 10000,
				AmountUsed:  0,
				Status:      "active",
				StartTime:   fixture.Epoch - 10,
				EndTime:     fixture.Epoch + 86400}).Error)
		require.NoError(t, env.DB.Model(&model.User{}).Where("id=1").Update("setting", `{"billing_preference":"subscription_only"}`).Error)
	}
	if tc.Variant == "quota_denied" {
		require.NoError(t, env.DB.Model(&model.User{}).Where("id = ?", 1).Update("quota", 0).Error)
	}
	if tc.Variant == "disabled_owner" {
		require.NoError(t, env.DB.Model(&model.User{}).Where("id = ?", 1).Update("status", common.UserStatusDisabled).Error)
		require.NoError(t, model.PublishUserAuthCache(1))
	}
	if tc.Variant == "duplicate" {
		require.NoError(t,
			env.DB.Create(&model.Task{TaskID: "task_own",
				UserId:    2,
				Status:    model.TaskStatusSuccess,
				CreatedAt: fixture.Epoch,
				UpdatedAt: fixture.Epoch}).Error)
	}
	if tc.Variant == "self_url" {
		require.NoError(t,
			env.DB.Model(&model.Task{}).Where("id = ?",
				81).Update("private_data",
				`{"result_url":"https://console.example.test/v1/videos/task_own/content"}`).Error)
	}
	if tc.Variant == "invalid_url" || tc.Variant == "data_url" {
		url := "ftp://203.0.113.10/video"
		if tc.Variant == "data_url" {
			url = "data:video/mp4;base64,dmlkZW8="
		}
		task, _, err := tasksapp.NewStore().GetByTaskId(1, "task_own")
		require.NoError(t, err)
		task.PrivateData.ResultURL = url
		require.NoError(t, tasksapp.NewStore().UpdateTask(task))
	}
	if tc.Kind == "plugin_artifacts" {
		items := []map[string]string{{"key": "video", "type": "video", "mimeType": "video/mp4"}}
		switch tc.Variant {
		case "invalid_key":
			items[0]["key"] = " bad"
		case "duplicate_key":
			items = append(items, items[0])
		case "invalid_type":
			items[0]["type"] = "unknown"
		case "invalid_mime":
			items[0]["mimeType"] = "video/mp4\r\ninvalid"
		case "too_many":
			items = nil
			for n := range 65 {
				items = append(items, map[string]string{"key": fmt.Sprintf("video%d", n), "type": "video"})
			}
		}
		encoded, err := common.Marshal(items)
		require.NoError(t, err)
		if tc.Variant != "unavailable" {
			source := `export const meta={apiVersion:1,key:"artifact-fixture",name:"Artifact fixture",version:"1.0.0",author:{name:"Contract fixture"},models:["artifact-model"],fetchMode:"per_task"};
 export function buildSubmitRequest(ctx){return {url:ctx.baseUrl+"/submit"}};
 export function parseSubmitResponse(){return {taskId:"fixture"}};
 export function buildQueryRequest(ctx){return {url:ctx.baseUrl+"/query"}};
 export function parseTaskResult(){return {status:"SUCCESS"}};
 export function listArtifacts(){return ` + string(encoded) + `};
 export function buildContentRequest(){throw new Error("artifact_not_found")};`
			_, err := pluginruntime.DefaultRegistry.Register(source, pluginruntime.Options{})
			require.NoError(t, err)
		}
		task, exists, err := tasksapp.NewStore().GetByTaskId(1, "task_own")
		require.NoError(t, err)
		require.True(t, exists)
		task.Platform = "artifact-fixture"
		task.PrivateData.Execution = &model.TaskExecutionSnapshot{TaskPlugin: &model.TaskPluginSnapshot{Key: "artifact-fixture"}}
		require.NoError(t, tasksapp.NewStore().UpdateTask(task))
	}
	if strings.HasPrefix(tc.Variant, "provenance") || tc.Variant == "reconciliation" {
		task, exists, err := tasksapp.NewStore().GetByTaskId(1, "task_own")
		require.NoError(t, err)
		require.True(t, exists)
		if strings.HasPrefix(tc.Variant, "provenance") {
			task.Properties = model.Properties{OriginModelName: "client-model", UpstreamModelName: "provider-model"}
			task.Data = []byte(`{"model":"provider-model","status":"ok"}`)
			task.PrivateData.UpstreamTaskID = "upstream-private"
			task.PrivateData.NodeName = "tasks-contract-node"
			task.PrivateData.Key = "private-fixture-key"
			task.PrivateData.Execution = &model.TaskExecutionSnapshot{RequestID: "fixture-request",
				RequestPath: "/v1/tasks/kling",
				TaskPlugin: &model.TaskPluginSnapshot{Key: "kling",
					Name:       "Kling",
					Version:    "1.0.2",
					APIVersion: 1,
					Generation: 7,
					Author:     &model.TaskPluginAuthorSnapshot{Name: "Fixture author"}}}
			if tc.Variant == "provenance_self_root" {
				task.UserId = 3
			}
		} else {
			task.Platform = constant.TaskPlatformOpenRouterAudio
			task.Action = "audio"
			task.Status = model.TaskStatusInProgress
			task.Progress = "30%"
			task.Quota = 50
			task.PrivateData.UpstreamTaskID = "gen-fixture"
			task.PrivateData.Key = "private-fixture-key"
			task.PrivateData.ResultURL = ""
			task.PrivateData.OpenRouterAudio = &model.OpenRouterSyncTask{RequestID: "private-request",
				NextPoll:       fixture.Epoch + 60,
				Deadline:       fixture.Epoch + 86400,
				ResultReady:    true,
				ResultMimeType: "audio/pcm",
				MetadataURL:    "https://private.invalid/v1/generation",
				Headers:        map[string][]string{"Authorization": {"Bearer private-fixture-key"}}}
			task.PrivateData.Reconciliation = &model.TaskReconciliation{ReservedQuota: 50, Required: true}
		}
		require.NoError(t, tasksapp.NewStore().UpdateTask(task))
		require.NoError(t, env.DB.Model(&model.Task{}).Where("id=81").UpdateColumn("updated_at", fixture.Epoch).Error)
	}
	if tc.Variant == "legacy_url" {
		require.NoError(t, env.DB.Model(&model.Task{}).Where("id=81").UpdateColumn("fail_reason", "https://203.0.113.10/private-result").Error)
	}
	if tc.Variant == "created_fallback" {
		require.NoError(t, env.DB.Model(&model.Task{}).Where("id=81").UpdateColumn("created_at", 0).Error)
	}
	if tc.Kind == "stored_artifact" {
		root := t.TempDir()
		require.NoError(t, os.Chmod(root, 0700))
		t.Setenv(system_setting.TaskArtifactStoreModeEnv, system_setting.TaskArtifactStoreModeFilesystem)
		t.Setenv(system_setting.TaskArtifactStoreRootEnv, root)
		require.NoError(t, service.InitTaskArtifactStore())
		t.Cleanup(func() {
			_ = os.Setenv(system_setting.TaskArtifactStoreModeEnv, system_setting.TaskArtifactStoreModeUpstream)
			_ = service.InitTaskArtifactStore()
		})
		task, exists, err := tasksapp.NewStore().GetByTaskId(1, "task_own")
		require.NoError(t, err)
		require.True(t, exists)
		reference,
			err := service.GetTaskArtifactStore().Persist(t.Context(),
			task,
			types.TaskArtifact{Key: "result",
				Type:     "audio",
				MimeType: "audio/pcm"},
			strings.NewReader("fixture audio"))
		require.NoError(t, err)
		task.Platform = constant.TaskPlatformOpenRouterAudio
		task.Status = model.TaskStatusInProgress
		task.Progress = "30%"
		task.PrivateData.ResultURL = ""
		task.PrivateData.OpenRouterAudio = &model.OpenRouterSyncTask{ResultReady: true,
			ResultMimeType: reference.MimeType,
			ResultSize:     reference.Size,
			ResultSHA256:   reference.SHA256}
		task.PrivateData.Reconciliation = &model.TaskReconciliation{ReservedQuota: 50, Required: true}
		require.True(t, filepath.IsLocal(reference.ObjectKey))
		blob := filepath.Join(root, reference.ObjectKey)
		switch tc.Variant {
		case "missing_blob":
			require.NoError(t, os.Remove(blob))
		case "corrupt_blob":
			require.NoError(t, os.WriteFile(blob, []byte("broken audio!"), 0600))
		case "invalid_checksum":
			task.PrivateData.OpenRouterAudio.ResultSHA256 = strings.Repeat("z", 64)
		case "disabled_store":
			t.Setenv(system_setting.TaskArtifactStoreModeEnv, system_setting.TaskArtifactStoreModeUpstream)
			require.NoError(t, service.InitTaskArtifactStore())
		}
		require.NoError(t, tasksapp.NewStore().UpdateTask(task))
		require.NoError(t, env.DB.Model(&model.Task{}).Where("id=81").UpdateColumn("updated_at", fixture.Epoch).Error)
	}
	// Fixture writes happen outside the fake-clock bubble; fix input timestamps before exercising production.
	require.NoError(t, env.DB.Model(&model.Task{}).Where("id > 0").UpdateColumn("updated_at", fixture.Epoch).Error)
	// Subscription HybridCache janitors must be initialized outside the synctest bubble.
	model.InvalidateSubscriptionPlanCache(31)
	if tc.Variant == "subscription" {
		require.NoError(t,
			env.DB.Model(&model.UserSubscription{}).Where("id=21").UpdateColumns(map[string]any{"created_at": fixture.Epoch,
				"updated_at": fixture.Epoch}).Error)
	}
	env.MountTasks()
	service.InitHttpClient()
	service.GetHttpClient().Transport = f.provider
	if client := service.GetSSRFProtectedHTTPClient(); client != nil {
		client.Transport = f.provider
	}
	if tc.Variant == "read_error" || tc.Variant == "count_error" || tc.Variant == "db_error" {
		require.NoError(t, env.DB.Callback().Query().Before("gorm:query").Register("tasks_contract_read_error", func(tx *gorm.DB) {
			if tx.Statement.Table == "tasks" || tx.Statement.Table == "midjourneys" {
				count := false
				_, count = tx.Statement.Dest.(*int64)
				if tc.Variant != "count_error" || count {
					tx.AddError(errors.New("fixture task query failure"))
				}
			}
		}))
	}
	if tc.Variant == "insert_error" {
		require.NoError(t, env.DB.Callback().Create().Before("gorm:create").Register("tasks_contract_insert_error", func(tx *gorm.DB) {
			if tx.Statement.Table == "tasks" {
				tx.AddError(errors.New("fixture task insert failure"))
			}
		}))
	}
	return f
}

func runTaskCase(t *testing.T, tc taskCase, updating bool) {
	f := newHTTPFixture(t, tc)
	synctest.Test(t, func(t *testing.T) {
		require.Equal(t, fixture.Epoch, time.Now().Unix())
		fixture.AssertDBClock(t)
		if tc.Plugin == "openrouter-video" {
			source, err := plugins.Source("openrouter-video")
			require.NoError(t, err)
			match := regexp.MustCompile(`"fetched_at": "([^"]+)"`).FindStringSubmatch(source)
			require.Len(t, match, 2)
			published, err := time.Parse(time.RFC3339Nano, match[1])
			require.NoError(t, err)
			time.Sleep(published.Add(time.Second).Sub(time.Now()))
		}
		for actor, identity := range f.identities {
			token, _, err := service.IssueAccessToken(identity)
			require.NoError(t, err)
			f.bearers[actor] = token
		}
		if tc.Kind == "provider_poll" {
			runProviderPollContract(t, f, tc, updating)
			return
		}
		if tc.Kind == "record" {
			runBillingContentContract(t, f, tc, updating)
			return
		}
		path := tc.Path
		actor := tc.Actor
		if tc.Kind == "mj_read" {
			switch tc.Variant {
			case "list":
				path = "/mj/task/list-by-condition"
				tc.Method = "POST"
				tc.Body = `{"ids":["mj_own","mj_foreign"]}`
			case "seed":
				path = "/mj/task/mj_own/image-seed"
				tc.Calls = 1
			case "image":
				path = "/mj/image/mj_own"
				actor = "anonymous"
				tc.Calls = 1
			case "notify", "notify_missing", "notify_invalid":
				tc.Method = "POST"
				tc.Body = `{"id":"mj_own","status":"SUCCESS","progress":"100%","finishTime":123}`
				if tc.Variant == "notify_missing" {
					tc.Body = `{"id":"missing"}`
				}
				if tc.Variant == "notify_invalid" {
					tc.Body = `{`
				}
			}
		}
		switch tc.Variant {
		case "foreign":
			path = strings.ReplaceAll(path, "task_own", "task_foreign")
		case "missing":
			path = strings.ReplaceAll(path, "task_own", "task_missing")
		case "anonymous":
			actor = "anonymous"
		case "dashboard_credential":
			actor = "user"
		}
		if tc.Kind == "capability" {
			access, err := service.IssueTaskArtifactAccess("task_own", "video")
			require.NoError(t, err)
			switch tc.Variant {
			case "tampered", "bearer_bad_access":
				access = "x" + access[1:]
			case "no_expiry":
				time.Sleep(10 * 365 * 24 * time.Hour)
			case "rotated":
				common.CryptoSecret = "rotated-fixture-secret"
			case "wrong_artifact":
				path = strings.ReplaceAll(path, "/video/content", "/missing/content")
			}
			path += "?access=" + access
			if tc.Variant == "bearer_bad_access" {
				actor = "api_key"
			}
		}
		req := httptest.NewRequest(tc.Method, path, strings.NewReader(tc.Body))
		if tc.Body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if tc.Variant == "multipart" {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			require.NoError(t, writer.SetBoundary("tasks-fixture-input-boundary"))
			for _, field := range []struct{ Key, Value string }{{"model", "sora-2"}, {"prompt", "fixture"}, {"seconds", "4"}} {
				require.NoError(t, writer.WriteField(field.Key, field.Value))
			}
			part, err := writer.CreateFormFile("input_reference", "fixture.png")
			require.NoError(t, err)
			_, err = io.WriteString(part, "fixture-image")
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			req = httptest.NewRequest(tc.Method, path, &body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
		}
		if bearer := f.bearers[actor]; bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		language := os.Getenv("TASKS_CONTRACT_LANGUAGE")
		if language == "" {
			language = "zh-CN"
		}
		if language != "default" {
			req.Header.Set("Accept-Language", language)
		}
		if tc.Kind == "media" {
			req.Header.Set("Range", "bytes=1-4")
			req.Header.Set("If-Range", "fixture-etag")
			req.Header.Set("If-None-Match", "fixture-etag")
		}
		var recorder *httptest.ResponseRecorder
		if tc.Kind == "mj_read" && strings.HasPrefix(tc.Variant, "notify") {
			recorder = httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = req
			result := relay.RelayMidjourneyNotify(c)
			c.JSON(200, result)
		} else {
			recorder = f.env.Serve(req)
		}
		synctest.Wait()
		require.Zero(t, gopool.WorkerCount(), "queued refund/cache effects must complete before snapshot")
		require.Equal(t, tc.Status, recorder.Code, recorder.Body.String())
		assert.NotContains(t, recorder.Body.String(), "private-fixture-key")
		if tc.Variant == "created_fallback" {
			assert.Contains(t, recorder.Body.String(), `"created_at":100`)
		}
		if tc.Variant == "legacy_url" {
			assert.NotContains(t, recorder.Body.String(), "private-result")
		}
		if tc.Variant == "provenance" || tc.Variant == "provenance_self_root" {
			if strings.Contains(tc.Path, "/self") {
				assert.NotContains(t, recorder.Body.String(), `"admin_info"`)
				assert.NotContains(t, recorder.Body.String(), `"root_info"`)
				assert.Contains(t, recorder.Body.String(), `"channel_id":0`)
			} else {
				assert.Contains(t, recorder.Body.String(), `"admin_info"`)
				if tc.Actor == "root" {
					assert.Contains(t, recorder.Body.String(), `"root_info"`)
				}
			}
		}
		if tc.Variant == "reconciliation" {
			assert.NotContains(t, recorder.Body.String(), "private.invalid")
			if !strings.HasSuffix(tc.Path, "/artifacts") {
				assert.Contains(t, recorder.Body.String(), `"replay_allowed":false`)
			}
		}

		require.Len(t, f.provider.calls, tc.Calls, "outbound: %+v; response: %s", f.provider.calls, recorder.Body.String())
		require.NoError(t, f.env.DB.Callback().Query().Remove("tasks_contract_read_error"))
		state := fixture.State(t)
		if tc.Variant == "subscription" {
			var user model.User
			require.NoError(t, f.env.DB.First(&user, 1).Error)
			require.Equal(t, 1000000, user.Quota)
			var sub model.UserSubscription
			require.NoError(t, f.env.DB.First(&sub, 21).Error)
			require.Equal(t, int64(500), sub.AmountUsed)
			require.Len(t, state["ledger"], 1)
		}
		expectedTasks := 2 + tc.NewTasks
		if tc.Variant == "empty" {
			expectedTasks = 0
		}
		if tc.Variant == "duplicate" {
			expectedTasks = 3
		}
		require.Len(t, state["tasks"], expectedTasks)
		if tc.Kind == "mj" {
			require.Len(t, state["midjourneys"], 3)
		}
		headers := recorder.Header().Clone()

		if tc.NewTasks > 0 && len(tc.Params) > 0 {
			for _, row := range state["logs"].([]map[string]any) {
				content, ok := row["content"].(string)
				require.True(t, ok)
				canonical, err := tasklog.CanonicalContent(content, "操作 text_to_video, 计算参数：", tc.Params)
				require.NoError(t, err)
				row["content"] = canonical
			}
		}

		result := map[string]any{"http": map[string]any{"status": recorder.Code,
			"headers": headers,
			"body":    recorder.Body.String()},
			"state":    state,
			"outbound": f.provider.calls}
		data, err := common.Marshal(result)
		require.NoError(t, err)
		if len(f.provider.calls) > 0 && (tc.Kind == "submit") && tc.NewTasks > 0 {
			var tasks []model.Task
			require.NoError(t, f.env.DB.Where("id > ?", 82).Find(&tasks).Error)
			for _, task := range tasks {
				old, new := fixture.BindTaskID(t, task.TaskID)
				data = []byte(strings.ReplaceAll(string(data), old, new))
			}
		}
		// Exact generated request identities are linked before their occurrences are replaced.
		requestID := recorder.Header().Get(common.RequestIdKey)
		if requestID != "" && requestID != "billing-contract-request" {
			require.Regexp(t, regexp.MustCompile(`^[0-9]{23}[A-Za-z0-9]{16}$`), requestID)
			data = []byte(strings.ReplaceAll(string(data), requestID, "<request-id>"))
		}

		fixture.Compare(t, "testdata/golden", tc.Name, fixture.StableRequestIDs(t, json.RawMessage(data)), *tasksPreflight, updating)
	})
}

func runBillingContentContract(t *testing.T, f *httpFixture, tc taskCase, updating bool) {
	task, exists, err := tasksapp.NewStore().GetByTaskId(1, "task_own")
	require.NoError(t, err)
	require.True(t, exists)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/tasks/hailuo", nil)
	c.Set(common.RequestIdKey, "tasks-content-fixture")
	info := &relaycommon.RelayInfo{UserId: 1,
		TokenId:         101,
		UsingGroup:      "default",
		OriginModelName: "MiniMax-H3",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 71},

		TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: constant.TaskActionTextToVideo},
		PriceData: types.PriceData{Quota: 17,
			ModelPrice:     0.01,
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}}}
	if tc.Variant == "ratios" {
		info.PriceData.AddOtherRatio("seconds", 5)
		info.PriceData.AddOtherRatio("resolution", 2)
	} else {
		info.TieredBillingSnapshot = &billingexpr.BillingSnapshot{TaskUsageBilling: true,
			ExprString:    `tier("base",u("seconds") * 0.4)`,
			EstimatedTier: "base",
			GroupRatio:    1,
			UsageFacts: map[string]any{"seconds": 6,
				"resolution":          "768P",
				"input_images":        0,
				"input_video_seconds": 0}}
	}
	events := []any{}
	for step := range 2 {
		require.NoError(t, taskrecord.LogTaskConsumption(c, info, task))
		state := fixture.State(t)
		rows := state["logs"].([]map[string]any)
		require.Len(t, rows, step+1)
		for _, row := range rows {
			content, ok := row["content"].(string)
			require.True(t, ok)
			row["content"], err = tasklog.CanonicalContent(content, "操作 text_to_video, 计算参数：", tc.Params)
			require.NoError(t, err)
		}
		var user model.User
		require.NoError(t, f.env.DB.First(&user, 1).Error)
		require.Equal(t, 17*(step+1), user.UsedQuota)
		require.Equal(t, 1000000, user.Quota)
		events = append(events, state)
	}
	fixture.Compare(t, "testdata/golden", tc.Name, fixture.StableRequestIDs(t, events), *tasksPreflight, updating)
}

func runProviderPollContract(t *testing.T, f *httpFixture, tc taskCase, updating bool) {
	task, found, err := tasksapp.NewStore().GetByTaskId(1, "task_own")
	require.NoError(t, err)
	require.True(t, found)
	task.Platform = constant.TaskPlatform(tc.Plugin)
	task.Status = model.TaskStatusInProgress
	task.Progress = "30%"
	task.Quota = 50
	task.SubmitTime = time.Now().Unix()
	task.Properties = model.Properties{OriginModelName: tc.Model, UpstreamModelName: tc.Model}
	task.PrivateData = model.TaskPrivateData{UpstreamTaskID: "upstream-fixture",
		BillingSource: "wallet",
		TokenId:       101,
		BillingContext: &model.TaskBillingContext{OriginModelName: tc.Model,
			PerCallBilling: true}}
	if tc.Plugin == "google" {
		task.PrivateData.UpstreamTaskID = base64.RawURLEncoding.EncodeToString([]byte("operations/upstream-fixture"))
	}
	if tc.Plugin == "vertex-ai" {
		task.PrivateData.UpstreamTaskID = base64.RawURLEncoding.EncodeToString([]byte("projects/test-project/locations/us-central1/publishers/google/models/veo-3.0-generate-001/operations/upstream-fixture"))
	}
	require.NoError(t, tasksapp.NewStore().UpdateTask(task))
	require.NoError(t, f.env.DB.Model(&model.User{}).Where("id=1").Updates(map[string]any{"quota": 999950, "used_quota": 50, "request_count": 1}).Error)
	require.NoError(t, f.env.DB.Model(&model.Token{}).Where("id=101").Updates(map[string]any{"remain_quota": 999950, "used_quota": 50}).Error)
	require.NoError(t, f.env.DB.Model(&model.Channel{}).Where("id=71").Update("used_quota", 50).Error)
	polling := taskruntime.NewPolling()
	events := []any{}
	for range 2 {
		summary := polling.RunTasks(t.Context(), nil)
		events = append(events, map[string]any{"state": fixture.State(t), "summary": summary, "outbound": append([]outgoing{}, f.provider.calls...)})
	}
	require.NoError(t, f.env.DB.First(task, 81).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
	require.Equal(t, 50, task.Quota)
	require.Len(t, f.provider.calls, tc.Calls)
	fixture.Compare(t, "testdata/golden", tc.Name, fixture.StableRequestIDs(t, events), *tasksPreflight, updating)
}

func writeTaskLocales(t *testing.T, destination string) {
	t.Helper()
	names := []string{"access_own_tasks_anonymous",
		"access_admin_tasks_user",
		"detail_foreign",
		"artifacts_foreign",
		"capability_tampered",
		"media_GET_invalid_url",
		"submit_kling_quota_denied"}
	result := map[string]json.RawMessage{}
	for _, language := range []string{"default", "en", "ru", "zh-CN", "zh-TW", "unknown"} {
		for _, name := range names {
			exe, err := os.Executable()
			require.NoError(t, err)
			cmd := exec.CommandContext(t.Context(), exe, "-test.run=^TestTasksContract$", "-test.count=1", "-test.timeout=90s", "-tasks-preflight")
			cmd.Env = append(os.Environ(), "TASKS_CONTRACT_CASE="+name, "TASKS_CONTRACT_LANGUAGE="+language, "TASKS_CONTRACT_LOCALE_EVIDENCE=", "TZ=UTC")
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", output)
			found := false
			for _, line := range strings.Split(string(output), "\n") {
				if raw, ok := strings.CutPrefix(line, "TASKS_SNAPSHOT "+name+" "); ok {
					require.False(t, found)
					found = true
					result[language+"/"+name] = json.RawMessage(raw)
				}
			}
			require.True(t, found, name)
		}
	}
	data, err := common.Marshal(result)
	require.NoError(t, err)
	data, err = common.IndentJson(data)
	require.NoError(t, err)
	stream, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	require.NoError(t, err)
	_, err = stream.Write(append(data, '\n'))
	require.NoError(t, err)
	require.NoError(t, stream.Close())
}
