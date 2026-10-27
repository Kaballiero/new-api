package contract_test

import (
	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"

	accountstorage "github.com/QuantumNous/new-api/internal/app/accountstorage"

	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/controller"
	authz "github.com/QuantumNous/new-api/internal/app/authorizationcore"
	settingsapp "github.com/QuantumNous/new-api/internal/app/settings"
	authzdomain "github.com/QuantumNous/new-api/internal/authorization/domain"
	"github.com/QuantumNous/new-api/internal/contracttest"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"

	authtoken "github.com/QuantumNous/new-api/internal/adapter/identity/authtoken"
)

const originCommit = "6b0f439a432ab129817e4e41ce323e6bd3c3c0c3"

var (
	auditManifest  = flag.Bool("audit-manifest", false, "print the complete case manifest without database access")
	auditPreflight = flag.Bool("audit-preflight", false, "check assertions without reading or writing goldens")
)

// Each case runs in a fresh process. Production limiters, authorization and
// session caches must not carry state between fixtures or recording repeats.
type auditCase struct {
	Name, Group, Actor, Method, Path, Body, Locale string
	Setup, Action, Category, Response              string
	Status, Rows, Total                            int
	Failed                                         bool
}

func auditCases() []auditCase {
	var cases []auditCase
	for _, endpoint := range []string{"/api/audit", "/api/audit/self"} {
		for _, actor := range []string{"anonymous", "user", "user_pat", "admin", "admin_pat", "allowed", "allowed_pat", "root", "root_pat"} {
			status, total := 200, 0
			if actor == "anonymous" {
				status = 401
			} else if endpoint == "/api/audit" {
				switch actor {
				case "user", "user_pat", "admin", "admin_pat":
					status = 403
				case "allowed", "allowed_pat":
					total = 4
				default:
					total = 8
				}
			} else if strings.HasPrefix(actor, "root") {
				total = 1
			} else {
				total = 2
			}
			rows := 0
			if strings.HasSuffix(actor, "pat") {
				rows = 1
			}
			cases = append(cases, auditCase{Name: strings.TrimPrefix(endpoint, "/api/") + "_" + actor,
				Group: "read", Actor: actor, Path: endpoint + "?end_timestamp=1000", Status: status,
				Rows: rows, Total: total, Failed: status != 200})
		}
	}
	for _, item := range []struct {
		name, query string
		root, own   int
		failed      bool
	}{
		{"all", "", 8, 2, false}, {"username", "username=contract-admin", 2, 2, false},
		{"literal_username", "username=contract-%25", 0, 2, false},
		{"login", "category=login", 2, 0, false}, {"security", "category=security", 3, 2, false},
		{"operation", "category=operation", 2, 0, false}, {"pat", "category=access_token", 1, 0, false},
		{"include", "token_ref=" + strings.Repeat("a", 64), 4, 1, false},
		{"exclude", "exclude_token_ref=" + strings.Repeat("a", 64), 4, 1, false},
		{"include_exclude", "token_ref=" + strings.Repeat("a", 64) + "&exclude_token_ref=" + strings.Repeat("a", 64), 0, 0, false},
		{"request", "request_id=seed-101", 1, 1, false}, {"empty", "request_id=absent", 0, 0, false},
		{"true", "success=true", 5, 1, false}, {"false", "success=false", 3, 1, false},
		{"empty_success", "success=", 8, 2, false},
		{"inclusive", "start_timestamp=100&end_timestamp=100", 2, 2, false},
		{"start", "start_timestamp=101", 5, 0, false}, {"end", "end_timestamp=99", 1, 0, false},
		{"zero_end", "start_timestamp=101&end_timestamp=0", 5, 0, false},
		{"zero", "start_timestamp=0&end_timestamp=0", 8, 2, false},
		{"max", "end_timestamp=9223372036854775807", 8, 2, false},
		{"max_start", "start_timestamp=9223372036854775807", 0, 0, false},
		{"page2", "p=2&page_size=1", 8, 2, false}, {"page_empty", "p=99&page_size=1", 8, 2, false},
		{"ps", "ps=1", 8, 2, false}, {"size", "size=1", 8, 2, false},
		{"precedence", "page_size=2&ps=1&size=3", 8, 2, false},
		{"page_fallback", "page_size=bad&ps=1&size=3", 8, 2, false},
		{"page_zero", "p=0&page_size=0&ps=0&size=1", 8, 2, false},
		{"page_cap", "page_size=101", 8, 2, false},
		{"page_max", "p=100000000", 8, 2, false},
		{"page_overflow", "p=9223372036854775808", 0, 0, true},
		{"repeated", "category=security&category=bad&success=false&success=true", 1, 1, false},
		{"ignored", "user_id=3&actor_role=100&action=absent&ip=absent&auth_method=bad&sort=asc&" +
			"token_id=999&type=99&channel=999&group=absent&model_name=absent", 8, 2, false},
		{"bad_category", "category=Security", 0, 0, true},
		{"bad_ref", "token_ref=secret", 0, 0, true},
		{"bad_exclude", "exclude_token_ref=" + strings.Repeat("A", 64), 0, 0, true},
		{"long_ref", "token_ref=" + strings.Repeat("a", 65), 0, 0, true},
		{"nonhex", "token_ref=" + strings.Repeat("g", 64), 0, 0, true},
		{"negative_time", "start_timestamp=-1", 0, 0, true},
		{"bad_time", "end_timestamp=bad", 0, 0, true},
		{"overflow_time", "end_timestamp=9223372036854775808", 0, 0, true},
		{"reverse", "start_timestamp=101&end_timestamp=100", 0, 0, true},
		{"bad_success", "success=1", 0, 0, true},
		{"negative_page", "p=-1", 0, 0, true},
		{"huge_page", "p=100000001", 0, 0, true},
		{"negative_size", "page_size=-1", 0, 0, true},
	} {
		for _, actor := range []string{"root", "user"} {
			path, total := "/api/audit", item.root
			if actor == "user" {
				path, total = "/api/audit/self", item.own
			}
			cases = append(cases, auditCase{Name: "filter_" + actor + "_" + item.name, Group: "read", Actor: actor,
				Path: path + "?" + item.query, Status: 200, Total: total, Failed: item.failed})
		}
	}
	for _, setup := range []string{
		"deny", "user_allow", "query_failure", "count_failure", "malformed", "empty_metadata", "null_metadata", "actor_deleted",
	} {
		actor, path, total, status, failed := "root", "/api/audit", 8, 200, false
		switch setup {
		case "deny":
			actor, total, status, failed = "allowed", 0, 403, true
		case "user_allow":
			actor, total, status, failed = "user", 0, 403, true
		case "query_failure", "count_failure", "malformed":
			total, failed = 0, true
		}
		cases = append(cases, auditCase{Name: "read_" + setup, Group: "read", Actor: actor, Path: path,
			Setup: setup, Total: total, Status: status, Failed: failed})
	}
	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		for _, actor := range []string{"user", "user_pat"} {
			rows := 0
			if actor == "user_pat" {
				rows = 1
			}
			cases = append(cases, auditCase{Name: "request_" + method + "_" + actor, Group: "request", Actor: actor,
				Method: method, Path: "/audit-contract/public/segment?password=query-secret", Rows: rows, Status: 200})
		}
	}
	for _, item := range []struct {
		name, actor, setup, response string
		status, rows                 int
	}{
		{"unknown", "unknown", "", "", 401, 0}, {"revoked", "user_pat", "revoked", "", 401, 0},
		{"disabled", "user_pat", "disabled", "", 401, 1}, {"public_disabled", "user_pat", "disabled_public", "", 200, 1},
		{"forbidden", "user_pat", "root_only", "", 403, 1}, {"missing", "user_pat", "not_found", "", 404, 1},
		{"redirect", "user_pat", "redirect", "", 301, 1}, {"rate", "user_pat", "rate", "", 429, 1},
		{"business_failure", "user_pat", "", "false", 200, 1}, {"no_success", "user_pat", "", "missing", 200, 1},
		{"null", "user_pat", "", "null", 200, 1}, {"non_json", "user_pat", "", "text", 200, 1},
		{"full_buffer", "user_pat", "", "full_false", 200, 1},
		{"truncated", "user_pat", "", "overflow_false", 200, 1},
		{"write_string", "user_pat", "", "write_string", 200, 1},
		{"auth_only", "user_pat", "auth_only", "", 200, 1},
		{"rotate", "user_pat", "rotate", "", 200, 1}, {"revoke_inflight", "user_pat", "revoke_inflight", "", 200, 1},
		{"demote", "root_pat", "demote", "", 200, 1}, {"delete", "root_pat", "delete", "", 200, 1},
		{"long_agent", "user_pat", "long_agent", "", 200, 1},
	} {
		cases = append(cases, auditCase{Name: "request_" + item.name, Group: "request", Actor: item.actor,
			Method: "GET", Path: "/audit-contract/private/:id", Setup: item.setup, Response: item.response,
			Status: item.status, Rows: item.rows, Failed: item.status >= 400 || item.response == "false"})
	}
	cases = append(cases, tokenCases()...)
	for _, key := range fallbackRoutes {
		parts := strings.SplitN(key, " ", 3)
		cases = append(cases, auditCase{Name: "fallback_" + strings.ReplaceAll(parts[2], ".", "_"), Group: "fallback",
			Actor: "root", Method: parts[0], Path: parts[1], Action: parts[2], Category: "operation", Rows: 1, Status: 200})
	}
	for _, item := range []struct {
		name, actor, method, response, setup string
		status, rows                         int
	}{
		{"generic", "root", "POST", "", "", 200, 1},
		{"read", "root", "GET", "", "", 200, 0},
		{"business_failure", "root", "POST", "false", "", 200, 1},
		{"http_failure", "root", "POST", "", "reject", 400, 1},
		{"permission", "admin", "POST", "", "permission", 403, 1},
		{"before_auth", "user", "POST", "", "", 403, 0},
		{"pat", "root_pat", "POST", "", "", 200, 2},
		{"full_buffer", "root", "POST", "full_false", "", 200, 1},
		{"overflow", "root", "POST", "overflow_false", "", 200, 1},
		{"marker", "root", "POST", "", "marker", 200, 0},
	} {
		cases = append(cases, auditCase{Name: "fallback_" + item.name, Group: "fallback", Actor: item.actor,
			Method: item.method, Path: "/audit-contract/admin/:id", Setup: item.setup, Response: item.response,
			Status: item.status, Rows: item.rows, Failed: item.status >= 400 || item.response == "false", Action: "generic"})
	}
	for _, action := range []string{"provision", "initialize"} {
		for _, scenario := range []string{"success", "session", "anonymous", "user", "denied", "malformed_config", "disabled", "invalid", "storage"} {
			path, body := "/api/getapi/users", `{"username":"issued-user","password":"fixture-password","display_name":"Issued"}`
			status, rows, actor := 201, 2, "admin_pat"
			if action == "initialize" {
				path, body, status = "/api/getapi/users/1/pat", `{"expected_username":"contract-user","apply":true}`, 200
			}
			switch scenario {
			case "session":
				actor, rows = "admin", 1
			case "anonymous":
				actor, status, rows = "anonymous", 401, 1
			case "user":
				actor, status = "user_pat", 401
			case "disabled":
				status = 401
			case "denied", "malformed_config":
				status = 403
			case "invalid":
				body, status = `{"password":"body-secret"}`, 400
			case "storage":
				status = 503
			}
			cases = append(cases, auditCase{Name: "getapi_" + action + "_" + scenario, Group: "getapi", Actor: actor,
				Method: "POST", Path: path, Body: body, Setup: scenario, Status: status, Rows: rows, Failed: status >= 400})
		}
	}
	for _, item := range []auditCase{
		{Name: "option", Actor: "root", Method: "PUT", Path: "/api/option/", Body: `{"key":"AuditContractOption","value":"option-value-secret"}`,
			Action: "option.update", Category: "operation", Rows: 1, Status: 200},
		{Name: "option_audit_failure", Actor: "root", Method: "PUT", Path: "/api/option/",
			Body: `{"key":"AuditContractOption","value":"option-value-secret"}`, Setup: "audit_failure", Rows: 0, Status: 200},
		{Name: "create_user", Actor: "root", Method: "POST", Path: "/api/user/",
			Body: `{"username":"audit-new-user","password":"fixture-password","role":1,"status":1}`, Action: "user.create", Rows: 1, Status: 200},
		{Name: "quota", Actor: "root", Method: "POST", Path: "/api/user/manage",
			Body: `{"id":1,"action":"add_quota","mode":"add","value":7}`, Action: "user.quota_add", Rows: 1, Status: 200},
		{Name: "quota_failure", Actor: "root", Method: "POST", Path: "/api/user/manage",
			Body: `{"id":1,"action":"add_quota","mode":"add","value":0}`, Action: "user.quota_add", Rows: 1, Status: 400, Failed: true},
		{Name: "account_delete_proof_failure", Actor: "user", Method: "DELETE", Path: "/api/user/self",
			Action: "user.account_delete", Rows: 1, Status: 403, Failed: true},
		{Name: "twofa_proof_failure", Actor: "user", Method: "POST", Path: "/api/user/2fa/setup", Rows: 0, Status: 403, Failed: true},
		{Name: "pat_generate_proof_failure", Actor: "user_pat", Method: "POST", Path: "/api/user/token", Rows: 1, Status: 403, Failed: true},
		{Name: "subscription", Actor: "root", Method: "POST", Path: "/api/subscription/admin/plans/11/subscriptions/reset",
			Body: `{"advance_reset_time":false}`, Setup: "subscription", Action: "subscription.plan_reset", Rows: 2, Status: 200},
		{Name: "login", Actor: "anonymous", Method: "POST", Path: "/api/user/login",
			Body: `{"username":"contract-user","password":"fixture-password"}`, Setup: "login", Action: "login", Rows: 1, Status: 200},
		{Name: "login_failure", Actor: "anonymous", Method: "POST", Path: "/api/user/login",
			Body: `{"username":"contract-user","password":"wrong-password-secret"}`, Setup: "login", Rows: 0, Status: 200, Failed: true},
	} {
		item.Group = "manual"
		item.Name = "manual_" + item.Name
		cases = append(cases, item)
	}
	for i := range cases {
		cases[i].Name = strings.ReplaceAll(cases[i].Name, "/", "_")
		cases[i].Locale = "zh-CN"
		if cases[i].Method == "" {
			cases[i].Method = "GET"
		}
	}
	return cases
}

// Explicit route keys and actions form the pre-move oracle, independently of
// the production map. Handlers are inert; middleware and auth are real.
var fallbackRoutes = []string{
	"POST /api/user/topup/complete user.topup_complete",
	"DELETE /api/user/:id/reset_passkey user.reset_passkey",
	"DELETE /api/user/:id/oauth/bindings/:provider_id user.oauth_unbind",
	"POST /api/option/payment_compliance option.payment_compliance",
	"POST /api/option/rest_model_ratio option.reset_ratio",
	"DELETE /api/option/channel_affinity_cache option.clear_affinity_cache",
	"POST /api/custom-oauth-provider/ custom_oauth.create",
	"PUT /api/custom-oauth-provider/:id custom_oauth.update",
	"DELETE /api/custom-oauth-provider/:id custom_oauth.delete",
	"DELETE /api/performance/disk_cache performance.clear_disk_cache",
	"POST /api/performance/gc performance.gc",
	"DELETE /api/performance/logs performance.clear_logs",
	"PUT /api/redemption/ redemption.update",
	"POST /api/redemption/batch redemption.delete_batch",
	"DELETE /api/redemption/:id redemption.delete",
	"DELETE /api/redemption/invalid redemption.delete_invalid",
	"POST /api/prefill_group/ prefill_group.create",
	"PUT /api/prefill_group/ prefill_group.update",
	"DELETE /api/prefill_group/:id prefill_group.delete",
	"POST /api/vendors/ vendor.create",
	"PUT /api/vendors/ vendor.update",
	"DELETE /api/vendors/:id vendor.delete",
	"POST /api/models/ model.create",
	"PUT /api/models/ model.update",
	"DELETE /api/models/:id model.delete",
	"POST /api/models/sync_upstream model.sync_upstream",
	"POST /api/deployments/ deployment.create",
	"PUT /api/deployments/:id deployment.update",
	"DELETE /api/deployments/:id deployment.delete",
	"POST /api/subscription/admin/plans subscription.plan_create",
	"PUT /api/subscription/admin/plans/:id subscription.plan_update",
	"POST /api/subscription/admin/bind subscription.bind",
	"POST /api/system-task/log-cleanup log.cleanup_start",
}

func tokenCases() []auditCase {
	cases := []auditCase{
		{Name: "create", Method: "POST", Path: "/api/token/", Body: `{"name":"created","expired_time":-1,"unlimited_quota":true}`,
			Status: 201, Action: "token.create"},
		{Name: "create_negative", Method: "POST", Path: "/api/token/", Body: `{"name":"attempt","remain_quota":-1}`,
			Status: 400, Failed: true, Action: "token.create"},
		{Name: "malformed", Method: "POST", Path: "/api/token/", Body: `{"key":"body-secret"`, Status: 400, Failed: true, Action: "token.create"},
		{Name: "create_storage", Method: "POST", Path: "/api/token/", Body: `{"name":"attempt","unlimited_quota":true}`,
			Setup: "token_storage", Status: 500, Failed: true, Action: "token.create"},
		{Name: "update", Method: "PUT", Path: "/api/token/",
			Body: `{"id":11,"name":"renamed","expired_time":-1,"unlimited_quota":true,"group":"default"}`, Action: "token.update"},
		{Name: "update_storage", Method: "PUT", Path: "/api/token/", Body: `{"id":11,"name":"renamed","unlimited_quota":true}`,
			Setup: "token_storage", Status: 500, Failed: true, Action: "token.update"},
		{Name: "unchanged", Method: "PUT", Path: "/api/token/",
			Body:   `{"id":11,"name":"owned","expired_time":-1,"remain_quota":100,"unlimited_quota":true,"group":"default","allow_ips":""}`,
			Action: "token.update"},
		{Name: "foreign_update", Method: "PUT", Path: "/api/token/", Body: `{"id":12,"name":"forged","unlimited_quota":true}`,
			Status: 404, Failed: true, Action: "token.update"},
		{Name: "status", Method: "PUT", Path: "/api/token/?status_only=false", Body: `{"id":11,"status":2}`, Action: "token.status_update"},
		{Name: "delete", Method: "DELETE", Path: "/api/token/11", Action: "token.delete"},
		{Name: "foreign_delete", Method: "DELETE", Path: "/api/token/12", Status: 404, Failed: true, Action: "token.delete"},
		{Name: "missing_delete", Method: "DELETE", Path: "/api/token/99999", Status: 404, Failed: true, Action: "token.delete"},
		{Name: "reveal", Method: "POST", Path: "/api/token/11/key", Action: "token.key_view"},
		{Name: "foreign_reveal", Method: "POST", Path: "/api/token/12/key", Status: 404, Failed: true, Action: "token.key_view"},
		{Name: "batch_delete", Method: "POST", Path: "/api/token/batch", Body: `{"ids":[11,11,12,99999]}`, Action: "token.delete_batch"},
		{Name: "batch_empty", Method: "POST", Path: "/api/token/batch", Body: `{"ids":[]}`, Status: 200, Failed: true, Action: "token.delete_batch"},
		{Name: "batch_reveal", Method: "POST", Path: "/api/token/batch/keys", Body: `{"ids":[11,11,12,99999]}`, Action: "token.key_view_batch"},
		{Name: "batch_none", Method: "POST", Path: "/api/token/batch/keys", Body: `{"ids":[12,99999]}`, Action: "token.key_view_batch"},
		{Name: "batch_reveal_empty", Method: "POST", Path: "/api/token/batch/keys", Body: `{"ids":[]}`,
			Status: 200, Failed: true, Action: "token.key_view_batch"},
		{Name: "read", Path: "/api/token/", Rows: -1},
		{Name: "anonymous", Actor: "anonymous", Method: "POST", Path: "/api/token/", Status: 401, Failed: true, Rows: -1},
		{Name: "audit_failure", Method: "POST", Path: "/api/token/", Body: `{"name":"created","unlimited_quota":true}`,
			Setup: "audit_failure", Status: 201, Rows: -1},
		{Name: "log_unavailable", Method: "POST", Path: "/api/token/", Body: `{"name":"created","unlimited_quota":true}`,
			Setup: "log_unavailable", Status: 201, Rows: -1},
	}
	ids := make([]int, 101)
	for i := range ids {
		ids[i] = 10000 + i
	}
	body, err := common.Marshal(map[string]any{"ids": ids})
	if err != nil {
		panic(err)
	}
	cases = append(cases, auditCase{Name: "bounded_batch", Method: "POST", Path: "/api/token/batch",
		Body: string(body), Action: "token.delete_batch"})
	for _, response := range []string{"full_false", "overflow_false", "overflow_success", "write_string"} {
		cases = append(cases, auditCase{Name: "buffer_" + response, Method: "POST", Path: "/api/token/",
			Setup: "buffer", Response: response, Action: "token.create", Failed: response == "full_false" || response == "overflow_false"})
	}
	for i := range cases {
		cases[i].Name = "token_" + cases[i].Name
		cases[i].Group, cases[i].Category = "token", "security"
		if cases[i].Actor == "" {
			cases[i].Actor = "user"
		}
		if cases[i].Status == 0 {
			cases[i].Status = 200
		}
		if cases[i].Rows == -1 {
			cases[i].Rows = 0
		} else {
			cases[i].Rows = 1
		}
	}
	for _, index := range []int{0, 8, 12, 16} {
		pat := cases[index]
		pat.Name, pat.Actor, pat.Rows = pat.Name+"_pat", "user_pat", 2
		cases = append(cases, pat)
	}
	for _, setup := range []string{"endpoint_rate", "global_rate"} {
		rows := 2
		if setup == "global_rate" {
			rows = 1
		}
		cases = append(cases, auditCase{Name: "token_" + setup, Group: "token", Actor: "user_pat",
			Method: "POST", Path: "/api/token/11/key", Setup: setup, Status: 429, Rows: rows,
			Action: "token.key_view", Failed: true})
	}
	return cases
}

func TestAuditContract(t *testing.T) {
	updating := flag.Lookup("update").Value.String() == "true"
	if updating {
		require.False(t, *auditPreflight)
		require.Equal(t, originCommit, os.Getenv("WAVE3B_ORIGIN_RECORD"))
		directory, err := os.Getwd()
		require.NoError(t, err)
		require.True(t, strings.HasPrefix(directory, "/dev/shm/wave3b-origin-main."), "never record on the candidate")
		marker, err := os.ReadFile(filepath.Join(directory, "../../../.audit-origin"))
		require.NoError(t, err, "host script must verify the pinned source before enabling recording")
		require.Equal(t, originCommit+"\n", string(marker))
	}
	cases := auditCases()
	names := map[string]bool{}
	for _, tc := range cases {
		require.False(t, names[tc.Name], "duplicate case %s", tc.Name)
		names[tc.Name] = true
		require.Equal(t, "zh-CN", tc.Locale, tc.Name)
	}
	if *auditManifest {
		data, err := common.Marshal(cases)
		require.NoError(t, err)
		fmt.Println("AUDIT_MANIFEST " + string(data))
		return
	}
	if destination := os.Getenv("WAVE3B_LOCALE_EVIDENCE"); destination != "" {
		require.False(t, updating)
		writeLocaleEvidence(t, destination)
		return
	}
	if name := os.Getenv("WAVE3B_CASE"); name != "" {
		for _, tc := range cases {
			if tc.Name == name {
				runAuditCase(t, tc)
				return
			}
		}
		t.Fatalf("unknown audit case %q", name)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			executable, err := os.Executable()
			require.NoError(t, err)
			command := exec.CommandContext(t.Context(), executable, "-test.run=^TestAuditContract$", "-test.count=1",
				"-test.timeout=3m", "-update="+strconv.FormatBool(updating), "-audit-preflight="+strconv.FormatBool(*auditPreflight))
			command.Env = append(os.Environ(), "WAVE3B_CASE="+tc.Name, "TZ=UTC")
			output, err := command.CombinedOutput()
			require.NoError(t, err, "%s", output)
			if *auditPreflight {
				for _, line := range strings.Split(string(output), "\n") {
					if strings.HasPrefix(line, "AUDIT_SNAPSHOT ") {
						fmt.Println(line)
					}
				}
			}
		})
	}
}

type auditFixture struct {
	env           *contracttest.Env
	log           *gorm.DB
	bearers       map[string]string
	auditAttempts *int
}

func newAuditFixture(t *testing.T) auditFixture {
	t.Helper()
	env := contracttest.New(t)
	oldMemory, oldBatch := common.MemoryCacheEnabled, common.BatchUpdateEnabled
	common.MemoryCacheEnabled, common.BatchUpdateEnabled = false, false
	t.Cleanup(func() { common.MemoryCacheEnabled, common.BatchUpdateEnabled = oldMemory, oldBatch })
	require.NoError(t, authz.Init(env.DB))
	logDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "audit-log.db")), &gorm.Config{})
	require.NoError(t, err)
	connection, err := logDB.DB()
	require.NoError(t, err)
	previousLog := model.LOG_DB
	model.LOG_DB = logDB
	t.Cleanup(func() { model.LOG_DB = previousLog; assert.NoError(t, connection.Close()) })
	require.NoError(t, logDB.AutoMigrate(&model.AuditLog{}, &model.Log{}))
	require.NoError(t, env.DB.Create(&model.AuditLog{Id: 9001, EventId: "main-sentinel", UserId: 1,
		ActorRole: 1, CreatedAt: 100, RequestId: "main-sentinel", Category: "security"}).Error)
	f := auditFixture{env: env, log: logDB, auditAttempts: new(int), bearers: map[string]string{
		"user": env.User, "admin": env.Admin, "allowed": env.Admin, "root": env.Root, "user_pat": env.UserPAT,
		"admin_pat": "audit-contract-admin-pat", "allowed_pat": "audit-contract-admin-pat",
		"root_pat": "audit-contract-root-pat", "unknown": "unknown-pat-secret",
	}}
	for id, pat := range map[int]string{2: f.bearers["admin_pat"], 3: f.bearers["root_pat"]} {
		require.NoError(t, env.DB.Model(&model.User{}).Where("id = ?", id).Update("access_token", pat).Error)
		require.NoError(t, accountstorage.Cache().PublishUserAuthCache(id))
	}
	for _, row := range []model.AuditLog{
		{Id: 101, UserId: 1, ActorRole: 1, CreatedAt: 100, Category: "security", TokenRef: strings.Repeat("a", 64)},
		{Id: 102, UserId: 1, ActorRole: 10, CreatedAt: 100, Category: "security", TokenRef: strings.Repeat("b", 64), Success: true},
		{Id: 103, UserId: 1, ActorRole: 100, CreatedAt: 101, Category: "operation", TokenRef: strings.Repeat("a", 64)},
		{Id: 104, UserId: 1, ActorRole: 0, CreatedAt: 102, Category: "login", Success: true},
		{Id: 105, UserId: 2, ActorRole: 10, CreatedAt: 99, Category: "access_token", TokenRef: strings.Repeat("a", 64), Success: true},
		{Id: 106, UserId: 3, ActorRole: 100, CreatedAt: 103, Category: "security", TokenRef: strings.Repeat("a", 64), Success: true},
		{Id: 107, UserId: 1, ActorRole: 99, CreatedAt: 104, Category: "operation", Success: true},
		{Id: 108, UserId: 2, ActorRole: 1, CreatedAt: 105, Category: "login", TokenRef: strings.Repeat("b", 64)},
	} {
		row.EventId, row.RequestId = fmt.Sprintf("event-%03d", row.Id), fmt.Sprintf("seed-%d", row.Id)
		row.Username = map[int]string{1: "contract-user", 2: "contract-admin", 3: "contract-root"}[row.UserId]
		row.Action, row.Content = "seed.action", "Retained audit event"
		payload := `{"op":{"action":"seed.action","params":{"large":9007199254740993,"nested":{"zero":0,"false":false}}},` +
			`"admin_info":{"admin_id":3,"admin_role":100},"audit_info":{"method":"POST","route":"/safe/:id",` +
			`"path":"/safe/:id","status":200,"success":false},"root_info":{"generation":18446744073709551615},` +
			`"login_method":"password","user_agent":"seed-agent"}`
		require.NoError(t, row.Other.Scan(payload))
		require.NoError(t, logDB.Create(&row).Error)
	}
	empty := ""
	for _, token := range []model.Token{
		{Id: 11, UserId: 1, Key: "owned-key-secret", Name: "owned", Status: 1, ExpiredTime: -1,
			RemainQuota: 100, UnlimitedQuota: true, Group: "default", AllowIps: &empty},
		{Id: 12, UserId: 2, Key: "foreign-key-secret", Name: "foreign-name-secret", Status: 1, ExpiredTime: -1},
	} {
		require.NoError(t, env.DB.Create(&token).Error)
	}
	return f
}

func configureAuditCase(t *testing.T, f auditFixture, tc auditCase) {
	t.Helper()
	if strings.HasPrefix(tc.Actor, "allowed") {
		require.NoError(t, authz.SetUserPermissions(2, authz.PermissionsMap{authzdomain.ResourceAudit: {authzdomain.ActionRead: true}}))
	}
	switch tc.Setup {
	case "deny":
		require.NoError(t, authz.SetUserPermissions(2, authz.PermissionsMap{authzdomain.ResourceAudit: {authzdomain.ActionRead: false}}))
	case "user_allow":
		require.NoError(t, authz.SetUserPermissions(1, authz.PermissionsMap{authzdomain.ResourceAudit: {authzdomain.ActionRead: true}}))
	case "disabled", "disabled_public":
		id := 1
		if tc.Group == "getapi" {
			id = 2
		}
		require.NoError(t, f.env.DB.Model(&model.User{}).Where("id = ?", id).Update("status", common.UserStatusDisabled).Error)
		require.NoError(t, accountstorage.Cache().PublishUserAuthCache(id))
	case "revoked":
		require.NoError(t, f.env.DB.Model(&model.User{}).Where("id = 1").Update("access_token", nil).Error)
	case "actor_deleted":
		require.NoError(t, f.env.DB.Delete(&model.User{}, 2).Error)
	case "malformed":
		require.NoError(t, f.log.Model(&model.AuditLog{}).Where("id = 101").Update("other", "{broken").Error)
	case "empty_metadata", "null_metadata":
		payload := "{}"
		if tc.Setup == "null_metadata" {
			payload = "null"
		}
		require.NoError(t, f.log.Model(&model.AuditLog{}).Where("id = 101").Update("other", payload).Error)
	case "query_failure":
		require.NoError(t, f.log.Callback().Query().Before("gorm:query").Register("audit-contract:query", func(tx *gorm.DB) {
			_, count := tx.Statement.Dest.(*int64)
			if tx.Statement.Table == "audit_logs" && !count {
				tx.AddError(errors.New("audit fixture query failure"))
			}
		}))
		t.Cleanup(func() { assert.NoError(t, f.log.Callback().Query().Remove("audit-contract:query")) })
	case "count_failure":
		require.NoError(t, f.log.Callback().Query().Before("gorm:query").Register("audit-contract:count", func(tx *gorm.DB) {
			_, count := tx.Statement.Dest.(*int64)
			if tx.Statement.Table == "audit_logs" && count {
				tx.AddError(errors.New("audit fixture count failure"))
			}
		}))
		t.Cleanup(func() { assert.NoError(t, f.log.Callback().Query().Remove("audit-contract:count")) })
	case "audit_failure":
		failAudit := func(tx *gorm.DB) {
			if tx.Statement.Table == "audit_logs" {
				*f.auditAttempts++
				tx.AddError(errors.New("audit fixture insert failure"))
			}
		}
		require.NoError(t, f.log.Callback().Create().Before("gorm:create").Register("audit-contract:insert", failAudit))
	case "token_storage":
		failToken := func(tx *gorm.DB) {
			if tx.Statement.Table == "tokens" {
				tx.AddError(errors.New("storage-error-secret"))
			}
		}
		require.NoError(t, f.env.DB.Callback().Create().Before("gorm:create").Register("audit-contract:token-create", failToken))
		require.NoError(t, f.env.DB.Callback().Update().Before("gorm:update").Register("audit-contract:token-update", failToken))
	case "subscription":
		require.NoError(t, f.env.DB.Create(&model.SubscriptionPlan{Id: 11, Title: "Audit Plan", TotalAmount: 100}).Error)
		require.NoError(t, f.env.DB.Create(&model.UserSubscription{Id: 11, UserId: 1, PlanId: 11, AmountTotal: 100,
			AmountUsed: 7, Status: "active", StartTime: 100, EndTime: 4102444800}).Error)
	case "login":
		hash, err := common.Password2Hash("fixture-password")
		require.NoError(t, err)
		require.NoError(t, f.env.DB.Model(&model.User{}).Where("id = 1").Update("password", hash).Error)
	}
	if tc.Group == "getapi" {
		capability := "getapi.users.provision"
		if strings.Contains(tc.Name, "initialize") {
			capability = "getapi.users.initialize-pat"
		}
		config := []map[string]any{{"integration_id": "fixture-integration", "principal_user_id": 2, "capabilities": []string{capability}}}
		if tc.Setup == "denied" {
			config[0]["capabilities"] = []string{}
		}
		data, err := common.Marshal(config)
		require.NoError(t, err)
		t.Setenv("GETAPI_INTEGRATIONS", string(data))
		if tc.Setup == "malformed_config" {
			t.Setenv("GETAPI_INTEGRATIONS", "{broken")
		}
		if tc.Setup == "storage" {
			require.NoError(t, f.env.DB.Callback().Create().Before("gorm:create").Register("audit-contract:users", func(tx *gorm.DB) {
				if tx.Statement.Table == "users" {
					tx.AddError(errors.New("storage-error-secret"))
				}
			}))
			if strings.Contains(tc.Name, "initialize") {
				require.NoError(t, f.env.DB.Model(&model.User{}).Where("id = 1").Update("access_token", nil).Error)
				require.NoError(t, f.env.DB.Callback().Update().Before("gorm:update").Register("audit-contract:target", func(tx *gorm.DB) {
					if tx.Statement.Table == "users" {
						tx.AddError(errors.New("storage-error-secret"))
					}
				}))
			}
		}
	}
	if tc.Group == "manual" && strings.HasPrefix(tc.Name, "manual_option") {
		settingsapp.Initialize()
	}
}

func auditRouter(t *testing.T, f auditFixture, tc auditCase) http.Handler {
	t.Helper()
	customToken := tc.Setup == "buffer" || tc.Setup == "endpoint_rate" || tc.Setup == "global_rate"
	if tc.Group != "fallback" && tc.Group != "request" && !customToken {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := f.env.Serve(r)
			for key, values := range response.Header() {
				w.Header()[key] = values
			}
			w.WriteHeader(response.Code)
			_, _ = w.Write(response.Body.Bytes())
		})
	}
	router := gin.New()
	router.Use(middleware.RequestId(), middleware.I18n())
	if tc.Setup != "auth_only" {
		router.Use(middleware.AccessTokenAudit())
	}
	if tc.Setup == "global_rate" {
		previous := common.GlobalApiRateLimitNum
		common.GlobalApiRateLimitNum = 1
		router.Use(middleware.GlobalAPIRateLimit())
		common.GlobalApiRateLimitNum = previous
	}
	path := strings.Split(tc.Path, "?")[0]
	if tc.Setup == "endpoint_rate" || tc.Setup == "global_rate" {
		path = "/api/token/:id/key"
	}
	if tc.Setup == "not_found" {
		router.NoRoute(func(c *gin.Context) { c.JSON(404, gin.H{"success": false}) })
		return router
	}
	var chain []gin.HandlerFunc
	if tc.Group == "fallback" {
		chain = append(chain, middleware.AdminAuth())
		if tc.Setup == "permission" {
			chain = append(chain, middleware.RequirePermission(authz.AuditRead))
		}
	} else if customToken {
		chain = append(chain, middleware.UserAuth(), middleware.TokenOperationAudit())
		if tc.Setup == "endpoint_rate" {
			previous := common.CriticalRateLimitNum
			common.CriticalRateLimitNum = 1
			chain = append(chain, middleware.CriticalRateLimit())
			common.CriticalRateLimitNum = previous
		}
	} else if !strings.Contains(tc.Name, "request_GET_") && !strings.Contains(tc.Name, "request_POST_") &&
		!strings.Contains(tc.Name, "request_PUT_") && !strings.Contains(tc.Name, "request_PATCH_") &&
		!strings.Contains(tc.Name, "request_DELETE_") && tc.Setup != "disabled_public" {
		if tc.Setup == "rate" {
			previous := common.GlobalApiRateLimitNum
			common.GlobalApiRateLimitNum = 1
			chain = append(chain, middleware.GlobalAPIRateLimit())
			common.GlobalApiRateLimitNum = previous
		}
		if tc.Setup == "root_only" {
			chain = append(chain, middleware.RootAuth())
		} else {
			chain = append(chain, middleware.UserAuth())
		}
	}
	chain = append(chain, func(c *gin.Context) {
		if tc.Setup == "endpoint_rate" || tc.Setup == "global_rate" {
			controller.GetTokenKey(c)
			return
		}
		switch tc.Setup {
		case "rotate", "revoke_inflight", "demote", "delete":
			id := c.GetInt("id")
			switch tc.Setup {
			case "rotate":
				require.NoError(t, f.env.DB.Model(&model.User{}).Where("id = ?", id).Update("access_token", "replacement-secret").Error)
			case "revoke_inflight":
				require.NoError(t, f.env.DB.Model(&model.User{}).Where("id = ?", id).Update("access_token", nil).Error)
			case "demote":
				require.NoError(t, f.env.DB.Model(&model.User{}).Where("id = ?", id).Update("role", 10).Error)
			case "delete":
				require.NoError(t, f.env.DB.Unscoped().Delete(&model.User{}, id).Error)
			}
		case "marker":
			common.SetContextKey(c, constant.ContextKeyAuditLogged, true)
		case "redirect":
			c.Redirect(301, "/safe-location")
			return
		case "reject":
			c.AbortWithStatusJSON(400, gin.H{"success": false})
			return
		}
		writeAuditResponse(c, tc.Response)
	})
	router.Handle(tc.Method, path, chain...)
	return router
}

func writeAuditResponse(c *gin.Context, response string) {
	switch response {
	case "false":
		c.JSON(200, gin.H{"success": false, "message": "response-error-secret"})
	case "missing":
		c.JSON(200, gin.H{"value": 7})
	case "null":
		c.JSON(200, gin.H{"success": nil})
	case "text":
		c.Data(200, "text/plain", []byte("fixture-text"))
	case "write_string":
		_, _ = c.Writer.WriteString(`{"success":false}`)
	case "full_false", "overflow_false", "overflow_success":
		length := 65536
		if response != "full_false" {
			length++
		}
		body := `{"padding":"` + strings.Repeat("x", length-len(`{"padding":"","success":false}`)) + `","success":false}`
		if response == "overflow_success" {
			common.SetContextKey(c, constant.ContextKeyTokenAuditSucceeded, true)
		}
		c.Data(200, "application/json; charset=utf-8", []byte(body))
	default:
		c.JSON(200, gin.H{"success": true})
	}
}

type httpSnapshot struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

func snapshotHTTP(response *httptest.ResponseRecorder) httpSnapshot {
	headers := map[string]string{}
	for _, key := range []string{"Cache-Control", "Pragma", "Expires", "Auth-Version", "Content-Type", "Retry-After", "Location", "Vary"} {
		if value := response.Header().Get(key); value != "" {
			headers[key] = value
		}
	}
	if response.Header().Get(common.RequestIdKey) != "" {
		headers[common.RequestIdKey] = "<request-id>"
	}
	return httpSnapshot{Status: response.Code, Headers: headers, Body: response.Body.String()}
}

func runAuditCase(t *testing.T, tc auditCase) {
	f := newAuditFixture(t)
	configureAuditCase(t, f, tc)
	router := auditRouter(t, f, tc)
	path := strings.NewReplacer(":id", "11", ":provider_id", "7").Replace(tc.Path)
	request := func() *http.Request {
		r := httptest.NewRequest(tc.Method, path, strings.NewReader(tc.Body))
		if token := f.bearers[tc.Actor]; token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		r.Header.Set("Accept-Language", tc.Locale)
		r.Header.Set("User-Agent", "audit-contract-agent")
		if tc.Setup == "long_agent" {
			r.Header.Set("User-Agent", strings.Repeat("\u00e9", 513))
		}
		if tc.Body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		r.RemoteAddr = "192.0.2.8:3456"
		return r
	}
	if tc.Setup == "rate" || tc.Setup == "endpoint_rate" || tc.Setup == "global_rate" {
		warmup := httptest.NewRecorder()
		router.ServeHTTP(warmup, request())
		require.Equal(t, 200, warmup.Code)
	}
	response := httptest.NewRecorder()
	before := time.Now().Unix()
	if tc.Setup == "log_unavailable" {
		model.LOG_DB = nil
	}
	router.ServeHTTP(response, request())
	model.LOG_DB = f.log
	require.Equal(t, tc.Status, response.Code, "%s: %s", tc.Name, response.Body.String())
	state := snapshotHTTP(response)
	var envelope struct {
		Success *bool `json:"success"`
		Data    struct {
			Total int               `json:"total"`
			Items []json.RawMessage `json:"items"`
		} `json:"data"`
	}
	if tc.Group == "read" {
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &envelope))
		require.NotNil(t, envelope.Success)
		require.Equal(t, !tc.Failed, *envelope.Success, response.Body.String())
		if !tc.Failed {
			require.Equal(t, tc.Total, envelope.Data.Total)
			require.NotNil(t, envelope.Data.Items, "empty pages must be []")
			if tc.Total > 0 && !strings.Contains(tc.Path, "p=99") && !strings.Contains(tc.Path, "p=100000000") {
				require.NotEmpty(t, envelope.Data.Items)
			}
			if len(envelope.Data.Items) > 0 {
				assert.Contains(t, state.Body, "9007199254740993", "nested integers must not pass through float64")
				if strings.Contains(tc.Path, "/self") || tc.Actor == "allowed" || tc.Actor == "allowed_pat" {
					assert.NotContains(t, state.Body, "root_info")
				}
				if strings.Contains(tc.Path, "/self") {
					assert.NotContains(t, state.Body, "admin_info")
					assert.NotContains(t, state.Body, "audit_info")
				}
			}
		}
	} else if common.Unmarshal(response.Body.Bytes(), &envelope) == nil && envelope.Success != nil {
		wantHTTP := !tc.Failed
		if tc.Response == "false" || tc.Response == "write_string" || strings.Contains(tc.Response, "false") || tc.Response == "overflow_success" {
			wantHTTP = false
		}
		require.Equal(t, wantHTTP, *envelope.Success, "HTTP result changed before golden comparison")
	}
	// Error callbacks apply only to the request, never to snapshot readback.
	if tc.Setup == "query_failure" {
		require.NoError(t, f.log.Callback().Query().Remove("audit-contract:query"))
	}
	if tc.Setup == "count_failure" {
		require.NoError(t, f.log.Callback().Query().Remove("audit-contract:count"))
	}
	var events []model.AuditLog
	require.NoError(t, f.log.Where("id > 108 AND request_id = ?", response.Header().Get(common.RequestIdKey)).Order("id").Find(&events).Error)
	require.Len(t, events, tc.Rows, "unexpected audit side effects: %s", tc.Name)
	if tc.Setup == "audit_failure" {
		require.Equal(t, 1, *f.auditAttempts, "failed manual audit must still suppress duplicate fallback")
	}
	categoryCounts := map[string]int{}
	seen := map[string]bool{}
	for i := range events {
		entry := &events[i]
		categoryCounts[entry.Category]++
		require.NotEmpty(t, entry.EventId)
		require.False(t, seen[entry.EventId], "event IDs must be unique")
		seen[entry.EventId] = true
		require.Equal(t, response.Header().Get(common.RequestIdKey), entry.RequestId)
		require.GreaterOrEqual(t, entry.CreatedAt, before)
		require.LessOrEqual(t, entry.CreatedAt, time.Now().Unix())
		require.Equal(t, tc.Method, entry.Method)
		require.Equal(t, tc.Status, entry.Status)
		wantSuccess := !tc.Failed
		switch tc.Response {
		case "false", "write_string", "full_false":
			wantSuccess = false
		case "overflow_false":
			wantSuccess = tc.Group != "token"
		case "overflow_success":
			wantSuccess = true
		}
		require.Equal(t, wantSuccess, entry.Success, "audit success policy changed")
		if entry.Category == "access_token" {
			require.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(f.bearers[tc.Actor]))), entry.TokenRef)
			require.Equal(t, "access_token", entry.AuthMethod)
		}
		if tc.Action != "" && entry.Category != "access_token" && !(tc.Setup == "subscription" && entry.UserId == 1) {
			require.Equal(t, tc.Action, entry.Action)
		}
		if tc.Setup == "subscription" && entry.UserId == 1 {
			require.Empty(t, entry.Action)
			require.Nil(t, entry.Other.Op)
		}
		if tc.Setup == "long_agent" {
			require.Equal(t, strings.Repeat("\u00e9", 512), entry.UserAgent)
		}
		if tc.Setup == "demote" || tc.Setup == "delete" {
			require.Equal(t, common.RoleRootUser, entry.ActorRole, "actor snapshot must survive live account changes")
		}
		entry.Id, entry.EventId, entry.RequestId, entry.CreatedAt = 0, fmt.Sprintf("<event-%d>", i+1), "<request-id>", 0
	}
	encoded, err := common.Marshal(events)
	require.NoError(t, err)
	wantAccess := 0
	if strings.HasSuffix(tc.Actor, "pat") && tc.Rows > 0 {
		wantAccess = 1
	}
	require.Equal(t, wantAccess, categoryCounts["access_token"])
	if others := tc.Rows - wantAccess; others > 0 {
		category := "operation"
		if tc.Group == "token" || tc.Name == "manual_account_delete_proof_failure" {
			category = "security"
		}
		if tc.Name == "manual_login" {
			category = "login"
		}
		require.Equal(t, others, categoryCounts[category])
	}
	for _, secret := range []string{f.env.User, f.env.Admin, f.env.Root, f.env.UserPAT, f.bearers["admin_pat"], f.bearers["root_pat"],
		"owned-key-secret", "foreign-key-secret", "foreign-name-secret", "option-value-secret", "query-secret", "body-secret",
		"response-error-secret", "storage-error-secret", "wrong-password-secret"} {
		assert.NotContains(t, string(encoded), secret)
	}
	var mainCount int64
	require.NoError(t, f.env.DB.Model(&model.AuditLog{}).Count(&mainCount).Error)
	require.EqualValues(t, 1, mainCount, "audit writes must use LOG_DB, not main")
	if tc.Group == "token" && tc.Method == "POST" && tc.Path == "/api/token/" && !tc.Failed && tc.Setup != "buffer" {
		var created model.Token
		require.NoError(t, f.env.DB.Where("name = ?", "created").Take(&created).Error)
		require.NotEmpty(t, created.Key)
		state.Body = normalizeTokenCreation(t, state.Body, created)
	}
	if tc.Group == "getapi" && !tc.Failed {
		state.Body = normalizeCredential(t, f, state.Body)
	}
	if tc.Name == "manual_login" {
		state.Body = normalizeLogin(t, f, state.Body)
	}
	usage := []struct {
		UserId  int    `json:"user_id"`
		Type    int    `json:"type"`
		Quota   int    `json:"quota"`
		Content string `json:"content"`
		Other   string `json:"other"`
	}{}
	require.NoError(t, f.log.Model(&model.Log{}).Order("id").Find(&usage).Error)
	if tc.Name == "manual_quota" {
		require.Len(t, usage, 1)
	} else if tc.Group == "manual" && strings.Contains(tc.Name, "quota_failure") {
		require.Empty(t, usage)
	}
	snapshot := struct {
		HTTP   httpSnapshot    `json:"http"`
		Events json.RawMessage `json:"events"`
		Usage  any             `json:"usage"`
	}{state, encoded, usage}
	if *auditPreflight {
		data, err := common.Marshal(snapshot)
		require.NoError(t, err)
		fmt.Printf("AUDIT_SNAPSHOT %s %x\n", tc.Name, sha256.Sum256(data))
		return
	}
	contracttest.GoldenValue(t, "audit_"+tc.Name, snapshot)
}

// Replace only validated generated response fields. Keep the raw JSON around
// these values so field order, empty values and nested integer lexemes are pinned.
func normalizeTokenCreation(t *testing.T, body string, token model.Token) string {
	t.Helper()
	var response struct {
		Data struct {
			ID       int    `json:"id"`
			Key      string `json:"key"`
			Created  int64  `json:"created_time"`
			Accessed int64  `json:"accessed_time"`
		} `json:"data"`
	}
	require.NoError(t, common.UnmarshalJsonStr(body, &response))
	require.Equal(t, token.Id, response.Data.ID)
	require.Equal(t, token.CreatedTime, response.Data.Created)
	require.Equal(t, token.AccessedTime, response.Data.Accessed)
	require.Equal(t, token.Key[:4]+"**********"+token.Key[len(token.Key)-4:], response.Data.Key)
	body = strings.Replace(body, `"key":"`+response.Data.Key+`"`, `"key":"<masked-generated-key>"`, 1)
	for _, field := range []string{"created_time", "accessed_time"} {
		body = replaceJSONNumber(t, body, field, 0)
	}
	return body
}

func replaceJSONNumber(t *testing.T, body, field string, replacement int64) string {
	t.Helper()
	prefix := `"` + field + `":`
	start := strings.Index(body, prefix)
	require.NotEqual(t, -1, start)
	start += len(prefix)
	end := start
	for end < len(body) && body[end] >= '0' && body[end] <= '9' {
		end++
	}
	value, err := strconv.ParseInt(body[start:end], 10, 64)
	require.NoError(t, err)
	require.Greater(t, value, int64(0))
	return body[:start] + strconv.FormatInt(replacement, 10) + body[end:]
}

func normalizeCredential(t *testing.T, f auditFixture, body string) string {
	t.Helper()
	var response struct {
		Data struct {
			Token   *string `json:"access_token"`
			UserID  int     `json:"user_id"`
			Outcome string  `json:"outcome"`
		} `json:"data"`
	}
	require.NoError(t, common.UnmarshalJsonStr(body, &response))
	if response.Data.Token == nil {
		return body
	}
	token := *response.Data.Token
	require.NotEmpty(t, token)
	var user model.User
	require.NoError(t, f.env.DB.Select("id", "access_token").First(&user, response.Data.UserID).Error)
	require.Equal(t, user.GetAccessToken(), token)
	if response.Data.Outcome == "" || response.Data.Outcome == "issued" {
		require.Regexp(t, "^[A-Za-z0-9+/]{32}$", token)
	}
	quoted, err := common.Marshal(token)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(body, string(quoted)))
	return strings.Replace(body, string(quoted), `"<generated-credential>"`, 1)
}

func normalizeLogin(t *testing.T, f auditFixture, body string) string {
	t.Helper()
	var response struct {
		Data struct {
			Token   string `json:"access_token"`
			Expires int64  `json:"access_expires_at"`
			Session struct {
				SID     string `json:"sid"`
				Created int64  `json:"created_at"`
				Active  int64  `json:"last_active_at"`
				Expires int64  `json:"expires_at"`
			} `json:"session"`
		} `json:"data"`
	}
	require.NoError(t, common.UnmarshalJsonStr(body, &response))
	identity, err := authtoken.ParseAccessToken(response.Data.Token)
	require.NoError(t, err)
	require.Equal(t, 1, identity.UserID)
	require.Equal(t, response.Data.Session.SID, identity.SessionID)
	var session identityrecord.UserSession
	require.NoError(t, f.env.DB.Where("sid = ?", identity.SessionID).Take(&session).Error)
	require.Equal(t, session.CreatedAt, response.Data.Session.Created)
	require.Equal(t, session.LastActiveAt, response.Data.Session.Active)
	require.Equal(t, session.ExpiresAt, response.Data.Session.Expires)
	require.Greater(t, response.Data.Expires, time.Now().Unix())
	body = replaceJSONPath(t, body, []string{"data", "access_token"}, `"<access-jwt>"`)
	body = replaceJSONPath(t, body, []string{"data", "access_expires_at"}, "0")
	body = replaceJSONPath(t, body, []string{"data", "session", "sid"}, `"<session-id>"`)
	for _, field := range []string{"created_at", "last_active_at", "expires_at"} {
		body = replaceJSONPath(t, body, []string{"data", "session", field}, "0")
	}
	return body
}

func replaceJSONPath(t *testing.T, body string, path []string, replacement string) string {
	t.Helper()
	var object map[string]json.RawMessage
	require.NoError(t, common.UnmarshalJsonStr(body, &object))
	value, exists := object[path[0]]
	require.True(t, exists, "missing response path %v", path)
	if len(path) > 1 {
		replacement = replaceJSONPath(t, string(value), path[1:], replacement)
	}
	key, err := common.Marshal(path[0])
	require.NoError(t, err)
	old := string(key) + ":" + string(value)
	require.Equal(t, 1, strings.Count(body, old), "ambiguous response path %v", path)
	return strings.Replace(body, old, string(key)+":"+replacement, 1)
}

func writeLocaleEvidence(t *testing.T, destination string) {
	f := newAuditFixture(t)
	evidence := map[string]httpSnapshot{}
	for _, locale := range []string{"default", "en", "ru", "zh-CN", "zh-TW"} {
		for _, query := range []string{"p=-1", "category=invalid", "start_timestamp=-1", "success=invalid"} {
			for _, endpoint := range []string{"/api/audit", "/api/audit/self"} {
				r := httptest.NewRequest("GET", endpoint+"?"+query, nil)
				r.Header.Set("Authorization", "Bearer "+f.env.Root)
				if locale != "default" {
					r.Header.Set("Accept-Language", locale)
				}
				response := f.env.Serve(r)
				require.Equal(t, 200, response.Code)
				evidence[locale+" "+endpoint+" "+query] = snapshotHTTP(response)
			}
		}
	}
	data, err := common.Marshal(evidence)
	require.NoError(t, err)
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	require.NoError(t, err, "locale evidence is write-once")
	_, err = file.Write(append(data, '\n'))
	require.NoError(t, err)
	require.NoError(t, file.Close())
}
