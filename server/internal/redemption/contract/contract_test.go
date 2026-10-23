// Package contract characterizes redemption HTTP and persisted effects on the original router.
package contract

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/contracttest"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

const originCommit = "6b0f439a432ab129817e4e41ce323e6bd3c3c0c3"

var (
	manifest  = flag.Bool("redemption-manifest", false, "print the complete case manifest without database IO")
	preflight = flag.Bool("redemption-preflight", false, "assert responses and state without reading or writing goldens")
)

type contractCase struct {
	Name, Group, Actor, Method, Path, Body, Setup, Locale string
	Status                                                int
	Success                                               bool
	IDs                                                   []int
	Total                                                 int
	Created                                               int
	Deleted                                               []int
	Credit                                                int
	RedeemedID                                            int
	Change                                                string
	Repeat                                                bool
}

// Each case declares observable outcomes independently of the implementation under test.
func contractCases() []contractCase {
	cases := []contractCase{}
	for _, route := range []struct{ name, method, path, body string }{
		{"list", "GET", "/api/redemption/", ""},
		{"search", "GET", "/api/redemption/search?keyword=alpha", ""},
		{"get", "GET", "/api/redemption/11", ""},
		{"create", "POST", "/api/redemption/", `{"name":"new","count":1,"quota":500}`},
		{"batch", "POST", "/api/redemption/batch", `{"ids":[11]}`},
		{"update", "PUT", "/api/redemption/", `{"id":11,"name":"changed","quota":700}`},
		{"invalid", "DELETE", "/api/redemption/invalid", ""},
		{"delete", "DELETE", "/api/redemption/11", ""},
		{"redeem", "POST", "/api/user/topup", `{"key":"00000000000000000000000000000011"}`},
	} {
		for _, actor := range []string{"anonymous", "user", "admin", "root", "user_pat", "admin_pat", "root_pat", "api_key"} {
			tc := contractCase{Name: "access_" + route.name + "_" + actor, Group: route.name, Actor: actor,
				Method: route.method, Path: route.path, Body: route.body, Status: 200, Success: true}
			switch {
			case actor == "anonymous" || actor == "api_key":
				tc.Status, tc.Success = 401, false
			case route.name != "redeem" && (actor == "user" || actor == "user_pat"):
				tc.Status, tc.Success = 403, false
			}
			if tc.Success {
				switch route.name {
				case "list":
					tc.IDs, tc.Total = []int{21, 20, 19, 18, 17, 16, 15, 14, 13, 12}, 11
				case "search":
					tc.IDs, tc.Total = []int{13, 12, 11}, 3
				case "create":
					tc.Created = 1
				case "batch", "delete":
					tc.Deleted = []int{11}
				case "invalid":
					tc.Deleted = []int{13, 14, 15, 20, 21}
				case "update":
					tc.Change = "changed"
				case "redeem":
					tc.Credit, tc.RedeemedID = 500, 11
				}
			}
			cases = append(cases, tc)
		}
	}
	for _, query := range []struct {
		name, path string
		total      int
		ids        []int
	}{
		{"list_page2", "/api/redemption/?p=2&page_size=3", 11, []int{18, 17, 16}},
		{"list_empty_page", "/api/redemption/?p=99&page_size=3", 11, []int{}},
		{"list_filters_ignored", "/api/redemption/?keyword=missing&status=3&user_id=999&key=missing", 11, []int{21, 20, 19, 18, 17, 16, 15, 14, 13, 12}},
		{"list_alias_ps", "/api/redemption/?ps=2", 11, []int{21, 20}},
		{"list_alias_size", "/api/redemption/?page_size=no&ps=0&size=2", 11, []int{21, 20}},
		{"list_precedence", "/api/redemption/?page_size=3&ps=1&size=2", 11, []int{21, 20, 19}},
		{"list_cap", "/api/redemption/?page_size=101", 11, []int{21, 20, 19, 18, 17, 16, 15, 14, 13, 12, 11}},
		{"list_bad_page", "/api/redemption/?p=no&page_size=2", 11, []int{21, 20}},
		{"list_negative_page", "/api/redemption/?p=-2&page_size=2", 11, []int{21, 20}},
		{"list_negative_size", "/api/redemption/?page_size=-1", 11, []int{21, 20, 19, 18, 17, 16, 15, 14, 13, 12, 11}},
		{"search_empty", "/api/redemption/search?page_size=100", 11, []int{21, 20, 19, 18, 17, 16, 15, 14, 13, 12, 11}},
		{"search_id_or_name", "/api/redemption/search?keyword=11", 3, []int{17, 16, 11}},
		{"search_prefix", "/api/redemption/search?keyword=alpha", 3, []int{13, 12, 11}},
		{"search_missing", "/api/redemption/search?keyword=missing", 0, []int{}},
		{"search_overflow", "/api/redemption/search?keyword=99999999999999999999999999", 0, []int{}},
		{"search_wildcard", "/api/redemption/search?keyword=%25&page_size=100", 11, []int{21, 20, 19, 18, 17, 16, 15, 14, 13, 12, 11}},
		{"search_underscore", "/api/redemption/search?keyword=alpha_", 3, []int{13, 12, 11}},
		{"search_unicode", "/api/redemption/search?keyword=%E6%B5%8B", 1, []int{18}},
		{"search_enabled", "/api/redemption/search?status=1", 5, []int{18, 17, 16, 12, 11}},
		{"search_disabled", "/api/redemption/search?status=2", 2, []int{20, 14}},
		{"search_used", "/api/redemption/search?status=3", 2, []int{21, 15}},
		{"search_expired", "/api/redemption/search?status=expired", 1, []int{13}},
		{"search_unknown", "/api/redemption/search?status=unknown&page_size=100", 11, []int{21, 20, 19, 18, 17, 16, 15, 14, 13, 12, 11}},
		{"search_noncanonical", "/api/redemption/search?status=01&page_size=100", 11, []int{21, 20, 19, 18, 17, 16, 15, 14, 13, 12, 11}},
		{"search_composite", "/api/redemption/search?keyword=alpha&status=1&page_size=1&p=2", 2, []int{11}},
		{"search_ignored", "/api/redemption/search?keyword=alpha&id=999&user_id=999&name=missing&sort=asc", 3, []int{13, 12, 11}},
		{"search_first_query", "/api/redemption/search?keyword=alpha&keyword=beta&status=1&status=2", 2, []int{12, 11}},
	} {
		group := "search"
		if strings.HasPrefix(query.name, "list_") {
			group = "list"
		}
		cases = append(cases, contractCase{Name: query.name, Group: group, Actor: "admin", Method: "GET", Path: query.path,
			Status: 200, Success: true, IDs: query.ids, Total: query.total})
	}
	for _, id := range []string{"999", "0", "-1", "bad", "99999999999999999999999999", "22"} {
		cases = append(cases, contractCase{Name: "get_" + strings.ReplaceAll(id, "-", "minus"), Group: "get", Actor: "admin",
			Method: "GET", Path: "/api/redemption/" + id, Status: 200})
	}
	for _, row := range []struct {
		name, body, setup string
		created           int
		success           bool
	}{
		{"batch2", `{"name":"new","count":2,"quota":500}`, "", 2, true},
		{"batch100", `{"name":"new","count":100,"quota":500}`, "", 100, true},
		{"count0", `{"name":"new","count":0,"quota":500}`, "", 0, false},
		{"count_negative", `{"name":"new","count":-1,"quota":500}`, "", 0, false},
		{"count101", `{"name":"new","count":101,"quota":500}`, "", 0, false},
		{"count_type", `{"name":"new","count":"2","quota":500}`, "", 0, false},
		{"name_empty", `{"name":"","count":1,"quota":500}`, "", 0, false},
		{"name20", `{"name":"` + strings.Repeat("\u6d4b", 20) + `","count":1,"quota":500}`, "", 1, true},
		{"name21", `{"name":"` + strings.Repeat("\u6d4b", 21) + `","count":1,"quota":500}`, "", 0, false},
		{"quota0", `{"name":"new","count":1,"quota":0}`, "", 0, false},
		{"quota_negative", `{"name":"new","count":1,"quota":-1}`, "", 0, false},
		{"quota_max", `{"name":"new","count":1,"quota":9007199254740991}`, "", 1, true},
		{"quota_overflow", `{"name":"new","count":1,"quota":9007199254740992}`, "", 0, false},
		{"quota_type", `{"name":"new","count":1,"quota":"bad"}`, "", 0, false},
		{"past", `{"name":"new","count":1,"quota":500,"expired_time":1}`, "", 0, false},
		{"future", `{"name":"new","count":1,"quota":500,"expired_time":4102444800}`, "", 1, true},
		{"malformed", `{`, "", 0, false},
		{"compliance", `{`, "compliance_off", 0, false},
		{"version", `{`, "compliance_version", 0, false},
		{"partial_first", `{"name":"new","count":2,"quota":500}`, "insert_fail_first", 0, false},
		{"partial_second", `{"name":"new","count":2,"quota":500}`, "insert_fail_second", 1, false},
		{"untrusted_fields", `{"id":99,"user_id":99,"key":"client-key","status":3,"name":"new","count":1,` +
			`"quota":500,"created_time":1,"redeemed_time":1,"used_user_id":99}`, "", 1, true},
	} {
		cases = append(cases, contractCase{Name: "create_" + row.name, Group: "create", Actor: "admin", Method: "POST",
			Path: "/api/redemption/", Body: row.body, Setup: row.setup, Status: 200, Success: row.success, Created: row.created})
	}
	for _, row := range []struct {
		name, path, body, setup, change string
		success                         bool
	}{
		{"fields", "", `{"id":11,"name":"changed","quota":700}`, "", "changed", true},
		{"empty_name", "", `{"id":11,"name":"","quota":500}`, "", "empty_name", true},
		{"long_name", "", `{"id":11,"name":"` + strings.Repeat("x", 21) + `","quota":500}`, "", "long_name", true},
		{"future", "", `{"id":11,"name":"changed","quota":700,"expired_time":4102444800}`, "", "future", true},
		{"status_ignored", "", `{"id":11,"name":"changed","quota":700,"status":3,"used_user_id":99,"key":"client-key"}`, "", "changed", true},
		{"quota0", "", `{"id":11,"quota":0}`, "", "", false},
		{"quota_negative", "", `{"id":11,"quota":-1}`, "", "", false},
		{"quota_max", "", `{"id":11,"name":"changed","quota":9007199254740991}`, "", "quota_max", true},
		{"quota_overflow", "", `{"id":11,"quota":9007199254740992}`, "", "", false},
		{"past", "", `{"id":11,"quota":500,"expired_time":1}`, "", "", false},
		{"missing_precedes_quota", "", `{"id":999,"quota":0}`, "", "", false},
		{"id0", "", `{"id":0,"quota":0}`, "", "", false},
		{"malformed", "", `{`, "", "", false},
		{"status_true", "?status_only=true", `{"id":11,"status":2}`, "", "disabled", true},
		{"status_false", "?status_only=false", `{"id":11,"status":3,"quota":-1,"expired_time":1}`, "", "used", true},
		{"status_zero", "?status_only=0", `{"id":11,"status":0}`, "", "status_zero", true},
		{"status_unknown", "?status_only=true", `{"id":11,"status":99}`, "", "status_unknown", true},
		{"status_empty", "?status_only=", `{"id":11,"status":2}`, "", "", false},
		{"status_loaded_quota", "?status_only=true", `{"id":11,"status":2}`, "stored_quota0", "", false},
		{"status_loaded_overflow", "?status_only=true", `{"id":11,"status":2}`, "stored_quota_overflow", "", false},
	} {
		cases = append(cases, contractCase{Name: "update_" + row.name, Group: "update", Actor: "admin", Method: "PUT",
			Path: "/api/redemption/" + row.path, Body: row.body, Setup: row.setup, Status: 200, Success: row.success, Change: row.change})
	}
	for _, row := range []struct {
		name, body string
		ids        []int
		success    bool
	}{
		{"duplicates_missing", `{"ids":[11,14,11,999]}`, []int{11, 14}, true},
		{"already_deleted", `{"ids":[22,999]}`, nil, true},
		{"used_disabled", `{"ids":[15,14]}`, []int{14, 15}, true},
		{"missing", `{}`, nil, false}, {"empty", `{"ids":[]}`, nil, false}, {"null", `{"ids":null}`, nil, false},
		{"zero", `{"ids":[0]}`, nil, false}, {"negative", `{"ids":[11,-1]}`, nil, false},
		{"string", `{"ids":["11"]}`, nil, false}, {"malformed", `{`, nil, false},
		{"count1000", `{"ids":[` + strings.Repeat("11,", 999) + `11]}`, []int{11}, true},
		{"count1001", `{"ids":[` + strings.Repeat("11,", 1000) + `11]}`, nil, false},
	} {
		cases = append(cases, contractCase{Name: "batch_" + row.name, Group: "batch", Actor: "admin", Method: "POST",
			Path: "/api/redemption/batch", Body: row.body, Status: 200, Success: row.success, Deleted: row.ids})
	}
	cases = append(cases, contractCase{Name: "batch_repeat", Group: "batch", Actor: "admin", Method: "POST", Path: "/api/redemption/batch",
		Body: `{"ids":[11,14,11,999]}`, Status: 200, Success: true, Deleted: []int{11, 14}, Repeat: true})
	for _, id := range []string{"999", "0", "bad", "-1", "99999999999999999999999999", "22"} {
		cases = append(cases, contractCase{Name: "delete_" + strings.ReplaceAll(id, "-", "minus"), Group: "delete", Actor: "admin",
			Method: "DELETE", Path: "/api/redemption/" + id, Status: 200})
	}
	cases = append(cases, contractCase{Name: "delete_repeat", Group: "delete", Actor: "admin", Method: "DELETE",
		Path: "/api/redemption/11", Status: 200, Success: true, Deleted: []int{11}, Repeat: true})
	for _, row := range []struct {
		name, key, body, setup string
		credit, id             int
		success                bool
	}{
		{"unknown", "unknown-code", "", "", 0, 0, false}, {"used", codeKey(15), "", "", 0, 0, false},
		{"disabled", codeKey(14), "", "", 0, 0, false}, {"expired", codeKey(13), "", "", 0, 0, false},
		{"disabled_expired", codeKey(20), "", "", 0, 0, false}, {"unknown_status", codeKey(19), "", "", 0, 0, false},
		{"soft_deleted", codeKey(22), "", "", 0, 0, false}, {"empty", "", `{"key":""}`, "", 0, 0, false},
		{"omitted", "", `{}`, "", 0, 0, false}, {"spaces", " ", "", "", 0, 0, false},
		{"malformed", "", `{`, "", 0, 0, false}, {"key_type", "", `{"key":1}`, "", 0, 0, false},
		{"compliance", "", `{`, "compliance_off", 0, 0, false}, {"version", "", `{`, "compliance_version", 0, 0, false},
		{"wallet_boundary", codeKey(11), "", "wallet_boundary", 500, 11, true},
		{"wallet_overflow", codeKey(11), "", "wallet_overflow", 0, 0, false},
		{"stored_quota0", codeKey(11), "", "stored_quota0", 0, 0, false},
		{"stored_quota_overflow", codeKey(11), "", "stored_quota_overflow", 0, 0, false},
		{"credit_failure", codeKey(11), "", "credit_failure", 0, 0, false},
		{"cas_failure", codeKey(11), "", "cas_failure", 0, 0, false},
		{"lookup_failure", codeKey(11), "", "lookup_failure", 0, 0, false},
		{"log_failure", codeKey(11), "", "log_failure", 500, 11, true},
	} {
		body := row.body
		if body == "" {
			b, err := common.Marshal(map[string]string{"key": row.key})
			if err != nil {
				panic(err)
			}
			body = string(b)
		}
		cases = append(cases, contractCase{Name: "redeem_" + row.name, Group: "redeem", Actor: "user", Method: "POST", Path: "/api/user/topup",
			Body: body, Setup: row.setup, Status: 200, Success: row.success, Credit: row.credit, RedeemedID: row.id})
	}
	cases = append(cases, contractCase{Name: "redeem_cas_zero", Group: "redeem", Actor: "user", Method: "POST",
		Path: "/api/user/topup", Body: `{"key":"00000000000000000000000000000011"}`, Setup: "cas_zero", Status: 200})
	cases = append(cases,
		contractCase{Name: "redeem_admission_before_bind", Group: "redeem", Actor: "user", Method: "POST",
			Path: "/api/user/topup", Body: `{`, Setup: "admission_busy", Status: 200},
		contractCase{Name: "redeem_critical_rate_before_bind", Group: "redeem", Actor: "user", Method: "POST",
			Path: "/api/user/topup", Body: `{`, Setup: "critical_rate", Status: 429})
	for _, actor := range []string{"user", "user_pat"} {
		cases = append(cases, contractCase{Name: "redeem_repeat_" + actor, Group: "redeem", Actor: actor, Method: "POST", Path: "/api/user/topup",
			Body: `{"key":"00000000000000000000000000000011"}`, Status: 200, Success: true, Credit: 500, RedeemedID: 11, Repeat: true})
	}
	for _, group := range []string{"list", "search"} {
		path := "/api/redemption/"
		if group == "search" {
			path = "/api/redemption/search"
		}
		for _, setup := range []string{"empty_codes", "query_failure"} {
			cases = append(cases, contractCase{Name: group + "_" + setup, Group: group, Actor: "admin", Method: "GET", Path: path,
				Setup: setup, Status: 200, Success: setup == "empty_codes", IDs: []int{}})
		}
	}
	for _, lang := range []string{"en", "ru", "zh-CN", "zh-TW", "unset"} {
		for _, group := range []string{"redeem", "create"} {
			tc := contractCase{Name: group + "_stored_language_" + lang, Group: group, Actor: "user", Method: "POST", Path: "/api/user/topup",
				Body: `{"key":"00000000000000000000000000000011"}`, Setup: "language_" + lang, Status: 200, Success: true, Credit: 500, RedeemedID: 11}
			if group == "create" {
				tc.Actor, tc.Path, tc.Body, tc.Created, tc.Credit, tc.RedeemedID = "admin", "/api/redemption/", `{"name":"new","count":1,"quota":500}`, 1, 0, 0
			}
			cases = append(cases, tc)
		}
	}
	for _, currency := range []string{"TOKENS", "CNY", "CUSTOM"} {
		for _, group := range []string{"create", "redeem"} {
			tc := contractCase{Name: group + "_currency_" + currency, Group: group, Actor: "admin", Method: "POST", Path: "/api/redemption/",
				Body: `{"name":"new","count":1,"quota":500}`, Setup: "currency_" + currency, Status: 200, Success: true, Created: 1}
			if group == "redeem" {
				tc.Actor, tc.Path, tc.Created, tc.Credit, tc.RedeemedID = "user", "/api/user/topup", 0, 500, 11
				tc.Body = `{"key":"00000000000000000000000000000011"}`
			}
			cases = append(cases, tc)
		}
	}
	for i := range cases {
		cases[i].Locale = "zh-CN"
	}
	return cases
}

func codeKey(id int) string { return fmt.Sprintf("%032d", id) }

func TestRedemptionContract(t *testing.T) {
	updating := flag.Lookup("update").Value.String() == "true"
	if updating {
		if path := os.Getenv("REDEMPTION_CONTRACT_CONTENT_RECORD"); path != "" {
			verifyContentRecording(t, path)
		} else {
			verifyOriginRecording(t)
		}
	}
	cases := contractCases()
	names := map[string]bool{}
	for _, tc := range cases {
		require.False(t, names[tc.Name], "duplicate case %s", tc.Name)
		names[tc.Name] = true
		require.Equal(t, "zh-CN", tc.Locale)
	}
	if *manifest {
		require.False(t, updating)
		encoded, err := common.Marshal(cases)
		require.NoError(t, err)
		fmt.Println("REDEMPTION_MANIFEST " + string(encoded))
		return
	}
	if destination := os.Getenv("REDEMPTION_CONTRACT_LOCALE_EVIDENCE"); destination != "" {
		require.False(t, updating)
		writeLocaleEvidence(t, destination)
		return
	}
	if name := os.Getenv("REDEMPTION_CONTRACT_CASE"); name != "" {
		for _, tc := range cases {
			if tc.Name == name {
				runContractCase(t, tc)
				return
			}
		}
		t.Fatalf("unknown redemption case %q", name)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			executable, err := os.Executable()
			require.NoError(t, err)
			command := exec.CommandContext(t.Context(), executable, "-test.run=^TestRedemptionContract$", "-test.count=1", "-test.timeout=3m",
				"-update="+strconv.FormatBool(updating), "-redemption-preflight="+strconv.FormatBool(*preflight))
			command.Env = append(os.Environ(), "REDEMPTION_CONTRACT_CASE="+tc.Name, "TZ=UTC")
			output, err := command.CombinedOutput()
			require.NoError(t, err, "%s", output)
			if *preflight {
				for _, line := range strings.Split(string(output), "\n") {
					if strings.HasPrefix(line, "REDEMPTION_SNAPSHOT ") {
						fmt.Println(line)
					}
				}
			}
		})
	}
}

// Content recording accepts only explicitly selected cases against the host-verified source manifest.
func verifyContentRecording(t *testing.T, path string) {
	t.Helper()
	require.False(t, *manifest || *preflight)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var gate struct {
		Root   string
		Cases  []string
		Inputs map[string]string
	}
	require.NoError(t, common.Unmarshal(data, &gate))
	require.Contains(t, gate.Cases, os.Getenv("REDEMPTION_CONTRACT_CASE"), "only approved content cases may be updated")
	directory, err := os.Getwd()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(gate.Root, "server/internal/redemption/contract"), directory)
	require.NotEmpty(t, gate.Inputs)
	for name, digest := range gate.Inputs {
		require.False(t, filepath.IsAbs(name))
		require.False(t, strings.Contains(name, ".."))
		data, err := os.ReadFile(filepath.Join(gate.Root, name))
		require.NoError(t, err)
		require.Equal(t, digest, fmt.Sprintf("%x", sha256.Sum256(data)), "localized recording input changed: %s", name)
	}
}

// Recording needs a host-verified archive, an explicit neutral path and a matching source manifest.
func verifyOriginRecording(t *testing.T) {
	t.Helper()
	require.False(t, *manifest || *preflight)
	require.Equal(t, originCommit, os.Getenv("REDEMPTION_CONTRACT_ORIGIN_RECORD"))
	root, err := filepath.EvalSymlinks(os.Getenv("REDEMPTION_CONTRACT_ORIGIN_DIR"))
	require.NoError(t, err)
	for directory := root; ; directory = filepath.Dir(directory) {
		_, statErr := os.Stat(filepath.Join(directory, ".git"))
		require.True(t, os.IsNotExist(statErr), "recording must not run inside a Git checkout")
		if filepath.Dir(directory) == directory {
			break
		}
	}
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "internal/redemption/contract"), cwd, "candidate recording is forbidden")
	marker, err := os.ReadFile(filepath.Join(root, ".redemption-origin"))
	require.NoError(t, err)
	require.Equal(t, originCommit+"\n", string(marker))
	require.FileExists(t, filepath.Join(root, "go.mod"))
	require.NoFileExists(t, filepath.Join(root, "server/go.mod"))
	manifestData, err := os.ReadFile(filepath.Join(root, ".redemption-source.sha256"))
	require.NoError(t, err)
	var expected map[string]string
	require.NoError(t, common.Unmarshal(manifestData, &expected))
	require.NotEmpty(t, expected)
	for name, digest := range expected {
		require.False(t, filepath.IsAbs(name))
		require.False(t, strings.Contains(name, ".."))
		data, err := os.ReadFile(filepath.Join(root, name))
		require.NoError(t, err)
		require.Equal(t, digest, fmt.Sprintf("%x", sha256.Sum256(data)), "origin source changed: %s", name)
	}
}

type fixture struct {
	env     *contracttest.Env
	log     *gorm.DB
	bearers map[string]string
	before  []model.Redemption
	quotas  []walletRow
}

type walletRow struct {
	ID    int `json:"id"`
	Quota int `json:"quota"`
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	env := contracttest.New(t)
	previousMemory, previousBatch := common.MemoryCacheEnabled, common.BatchUpdateEnabled
	previousItems, previousUnit := common.ItemsPerPage, common.QuotaPerUnit
	previousPayment, previousGeneral := *operation_setting.GetPaymentSetting(), *operation_setting.GetGeneralSetting()
	previousExchange := operation_setting.USDExchangeRate
	common.MemoryCacheEnabled, common.BatchUpdateEnabled = false, false
	common.ItemsPerPage, common.QuotaPerUnit = 10, 500000
	payment := operation_setting.GetPaymentSetting()
	payment.ComplianceConfirmed = true
	payment.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	general := operation_setting.GetGeneralSetting()
	general.QuotaDisplayType = "USD"
	general.CustomCurrencySymbol = "C"
	general.CustomCurrencyExchangeRate = 2
	operation_setting.USDExchangeRate = 7
	i18n.SetUserLangLoader(model.GetUserLanguage)
	t.Cleanup(func() {
		common.MemoryCacheEnabled, common.BatchUpdateEnabled = previousMemory, previousBatch
		common.ItemsPerPage, common.QuotaPerUnit = previousItems, previousUnit
		*operation_setting.GetPaymentSetting(), *operation_setting.GetGeneralSetting() = previousPayment, previousGeneral
		operation_setting.USDExchangeRate = previousExchange
	})
	logDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "redemption-log.db")), &gorm.Config{})
	require.NoError(t, err)
	connection, err := logDB.DB()
	require.NoError(t, err)
	previousLog := model.LOG_DB
	model.LOG_DB = logDB
	t.Cleanup(func() { model.LOG_DB = previousLog; assert.NoError(t, connection.Close()) })
	require.NoError(t, logDB.AutoMigrate(&model.Log{}, &model.AuditLog{}))
	require.NoError(t, env.DB.Create(&model.Log{Id: 9001, UserId: 1, Type: 1, Content: "main-sentinel", RequestId: "main-sentinel"}).Error)
	require.NoError(t, env.DB.Create(&model.AuditLog{Id: 9001, EventId: "main-sentinel", UserId: 1, ActorRole: 1,
		CreatedAt: 100, Category: "security", RequestId: "main-sentinel"}).Error)
	f := fixture{env: env, log: logDB, bearers: map[string]string{"user": env.User, "admin": env.Admin, "root": env.Root,
		"user_pat": env.UserPAT, "admin_pat": "redemption-admin-pat", "root_pat": "redemption-root-pat", "api_key": "sk-redemption-api-key"}}
	for id, pat := range map[int]string{2: f.bearers["admin_pat"], 3: f.bearers["root_pat"]} {
		require.NoError(t, env.DB.Model(&model.User{}).Where("id = ?", id).Update("access_token", pat).Error)
		require.NoError(t, model.PublishUserAuthCache(id))
	}
	require.NoError(t, env.DB.Create(&model.Token{Id: 101, UserId: 1, Key: "redemption-api-key", Status: 1, ExpiredTime: -1,
		UnlimitedQuota: true, Name: "gateway-key", Group: "default"}).Error)
	rows := []model.Redemption{
		{Id: 11, Name: "alpha-active", Quota: 500, Status: 1},
		{Id: 12, Name: "alpha-future", Quota: 700, Status: 1, ExpiredTime: 4102444800},
		{Id: 13, Name: "alpha-expired", Quota: 900, Status: 1, ExpiredTime: 1},
		{Id: 14, Name: "beta-disabled", Quota: 1000, Status: 2},
		{Id: 15, Name: "beta-used", Quota: 1100, Status: 3, RedeemedTime: 102, UsedUserId: 2},
		{Id: 16, Name: "11-prefix", Quota: 1200, Status: 1},
		{Id: 17, Name: "111-numeric", Quota: 1300, Status: 1},
		{Id: 18, Name: "\u6d4b\u8bd5", Quota: 1400, Status: 1},
		{Id: 19, Name: "unknown-expired", Quota: 1500, Status: 9, ExpiredTime: 1},
		{Id: 20, Name: "disabled-expired", Quota: 1600, Status: 2, ExpiredTime: 1},
		{Id: 21, Name: "used-future", Quota: 1700, Status: 3, ExpiredTime: 4102444800, RedeemedTime: 103, UsedUserId: 3},
		{Id: 22, Name: "soft-deleted", Quota: 1800, Status: 1, DeletedAt: gorm.DeletedAt{Time: time.Unix(50, 0).UTC(), Valid: true}},
	}
	for i := range rows {
		rows[i].UserId, rows[i].Key, rows[i].CreatedTime = 2, codeKey(rows[i].Id), 100
	}
	require.NoError(t, env.DB.Create(&rows).Error)
	return f
}

func configureCase(t *testing.T, f fixture, tc contractCase) {
	t.Helper()
	switch tc.Setup {
	case "compliance_off":
		operation_setting.GetPaymentSetting().ComplianceConfirmed = false
	case "compliance_version":
		operation_setting.GetPaymentSetting().ComplianceTermsVersion = "obsolete"
	case "wallet_boundary", "wallet_overflow":
		balance := common.MaxWalletQuota - 500
		if tc.Setup == "wallet_overflow" {
			balance++
		}
		require.NoError(t, f.env.DB.Model(&model.User{}).Where("id = 1").Update("quota", balance).Error)
	case "stored_quota0", "stored_quota_overflow":
		quota := 0
		if tc.Setup == "stored_quota_overflow" {
			quota = common.MaxWalletQuota + 1
		}
		require.NoError(t, f.env.DB.Model(&model.Redemption{}).Where("id = 11").Update("quota", quota).Error)
	case "empty_codes":
		require.NoError(t, f.env.DB.Model(&model.Redemption{}).Where("1 = 1").
			Update("deleted_at", time.Unix(60, 0).UTC()).Error)
	case "insert_fail_first", "insert_fail_second":
		attempts := 0
		failAt := 1
		if tc.Setup == "insert_fail_second" {
			failAt = 2
		}
		require.NoError(t, f.env.DB.Callback().Create().Before("gorm:create").Register("redemption_contract_insert", func(tx *gorm.DB) {
			if tx.Statement.Table == "redemptions" {
				attempts++
				if attempts == failAt {
					tx.AddError(errors.New("injected redemption insert failure"))
				}
			}
		}))
		t.Cleanup(func() { assert.NoError(t, f.env.DB.Callback().Create().Remove("redemption_contract_insert")) })
	case "query_failure", "lookup_failure":
		require.NoError(t, f.env.DB.Callback().Query().Before("gorm:query").Register("redemption_contract_query", func(tx *gorm.DB) {
			if tx.Statement.Table == "redemptions" {
				tx.AddError(errors.New("injected redemption query failure"))
			}
		}))
		t.Cleanup(func() { assert.NoError(t, f.env.DB.Callback().Query().Remove("redemption_contract_query")) })
	case "credit_failure", "cas_failure":
		table := "users"
		if tc.Setup == "cas_failure" {
			table = "redemptions"
		}
		require.NoError(t, f.env.DB.Callback().Update().Before("gorm:update").Register("redemption_contract_update", func(tx *gorm.DB) {
			if tx.Statement.Table == table {
				tx.AddError(errors.New("injected redemption update failure"))
			}
		}))
		t.Cleanup(func() { assert.NoError(t, f.env.DB.Callback().Update().Remove("redemption_contract_update")) })
	case "cas_zero":
		require.NoError(t, f.env.DB.Callback().Update().After("gorm:update").Register("redemption_contract_cas_zero", func(tx *gorm.DB) {
			if tx.Statement.Table == "redemptions" && tx.Error == nil {
				tx.RowsAffected = 0
			}
		}))
		t.Cleanup(func() { assert.NoError(t, f.env.DB.Callback().Update().Remove("redemption_contract_cas_zero")) })
	case "log_failure":
		require.NoError(t, f.log.Callback().Create().Before("gorm:create").Register("redemption_contract_log", func(tx *gorm.DB) {
			if tx.Statement.Table == "logs" {
				tx.AddError(errors.New("injected redemption log failure"))
			}
		}))
		t.Cleanup(func() { assert.NoError(t, f.log.Callback().Create().Remove("redemption_contract_log")) })
	}
	if strings.HasPrefix(tc.Setup, "language_") {
		lang := strings.TrimPrefix(tc.Setup, "language_")
		if lang == "unset" {
			lang = ""
		}
		encoded, err := common.Marshal(map[string]string{"language": lang})
		require.NoError(t, err)
		require.NoError(t, f.env.DB.Model(&model.User{}).Where("id IN ?", []int{1, 2, 3}).Update("setting", string(encoded)).Error)
	}
	if strings.HasPrefix(tc.Setup, "currency_") {
		operation_setting.GetGeneralSetting().QuotaDisplayType = strings.TrimPrefix(tc.Setup, "currency_")
	}
}

type httpSnapshot struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

type stepSnapshot struct {
	HTTP    httpSnapshot    `json:"http"`
	Codes   json.RawMessage `json:"codes"`
	Wallets []walletRow     `json:"wallets"`
	Logs    json.RawMessage `json:"logs"`
	Events  json.RawMessage `json:"events"`
}

func snapshotHTTP(response *httptest.ResponseRecorder) httpSnapshot {
	headers := map[string]string{}
	for _, name := range []string{"Cache-Control", "Pragma", "Expires", "Auth-Version", "Content-Type", "Retry-After", "Location", "Vary"} {
		if value := response.Header().Get(name); value != "" {
			headers[name] = value
		}
	}
	body := response.Body.String()
	if id := response.Header().Get(common.RequestIdKey); id != "" {
		headers[common.RequestIdKey] = "<http-request>"
		body = strings.ReplaceAll(body, id, "<http-request>")
	}
	return httpSnapshot{response.Code, headers, body}
}

func readCodes(t *testing.T, db *gorm.DB) []model.Redemption {
	t.Helper()
	rows := []model.Redemption{}
	require.NoError(t, db.Unscoped().Order("id").Find(&rows).Error)
	return rows
}

func readWallets(t *testing.T, db *gorm.DB) []walletRow {
	t.Helper()
	rows := []walletRow{}
	require.NoError(t, db.Model(&model.User{}).Order("id").Find(&rows).Error)
	return rows
}

func runContractCase(t *testing.T, tc contractCase) {
	f := newFixture(t)
	// Capture configured state before fault callbacks are installed.
	if tc.Setup == "query_failure" || tc.Setup == "lookup_failure" {
		f.before = readCodes(t, f.env.DB)
		f.quotas = readWallets(t, f.env.DB)
		configureCase(t, f, tc)
	} else {
		configureCase(t, f, tc)
		f.before = readCodes(t, f.env.DB)
		f.quotas = readWallets(t, f.env.DB)
	}
	if tc.Setup == "critical_rate" {
		for range 20 {
			r := httptest.NewRequest("POST", tc.Path, strings.NewReader(`{"key":""}`))
			r.Header.Set("Authorization", "Bearer "+f.env.User)
			r.Header.Set("Content-Type", "application/json")
			r.RemoteAddr = "192.0.2.8:3456"
			response := f.env.Serve(r)
			require.Equal(t, 200, response.Code)
		}
	}
	steps := []stepSnapshot{}
	repeats := 1
	if tc.Repeat {
		repeats = 2
	}
	for index := range repeats {
		request := httptest.NewRequest(tc.Method, tc.Path, strings.NewReader(tc.Body))
		request.Header.Set("Accept-Language", tc.Locale)
		request.Header.Set("User-Agent", "redemption-contract-agent")
		request.RemoteAddr = "192.0.2.8:3456"
		if tc.Body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		if bearer := f.bearers[tc.Actor]; bearer != "" {
			request.Header.Set("Authorization", "Bearer "+bearer)
		}
		before := time.Now().Unix()
		var response *httptest.ResponseRecorder
		if tc.Setup == "admission_busy" {
			response = admissionRejection(t, f, request)
		} else {
			response = f.env.Serve(request)
		}
		require.Equal(t, tc.Status, response.Code, "%s", response.Body.String())
		var envelope struct {
			Success *bool           `json:"success"`
			Data    json.RawMessage `json:"data"`
		}
		if tc.Status == 429 {
			require.Empty(t, response.Body.Bytes())
			require.Equal(t, "1200", response.Header().Get("Retry-After"))
		} else {
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &envelope))
			require.NotNil(t, envelope.Success)
		}
		wantSuccess := tc.Success
		if index > 0 && (tc.Group == "redeem" || tc.Group == "delete") {
			wantSuccess = false
		}
		if tc.Status != 429 {
			require.Equal(t, wantSuccess, *envelope.Success, "%s", response.Body.String())
		}
		if tc.Setup == "query_failure" || tc.Setup == "lookup_failure" {
			require.NoError(t, f.env.DB.Callback().Query().Remove("redemption_contract_query"))
		}
		if tc.Success && (tc.Group == "list" || tc.Group == "search") {
			var page struct {
				Total int `json:"total"`
				Items []struct {
					ID int `json:"id"`
				} `json:"items"`
			}
			require.NoError(t, common.Unmarshal(envelope.Data, &page))
			require.Equal(t, tc.Total, page.Total)
			ids := []int{}
			for _, item := range page.Items {
				ids = append(ids, item.ID)
			}
			require.Equal(t, tc.IDs, ids)
		}
		codes := readCodes(t, f.env.DB)
		assertCodeEffects(t, f, tc, codes, index, before)
		wallets := readWallets(t, f.env.DB)
		expectedWallets := slices.Clone(f.quotas)
		actorID := 1
		if strings.HasPrefix(tc.Actor, "admin") {
			actorID = 2
		}
		if strings.HasPrefix(tc.Actor, "root") {
			actorID = 3
		}
		for i := range expectedWallets {
			if expectedWallets[i].ID == actorID {
				expectedWallets[i].Quota += tc.Credit
			}
		}
		require.Equal(t, expectedWallets, wallets, "exact wallet side effects")
		if tc.Credit > 0 && index == 0 {
			var quota int
			require.NoError(t, common.Unmarshal(envelope.Data, &quota))
			require.Equal(t, tc.Credit, quota)
		}
		if tc.Group == "batch" && wantSuccess {
			var count int
			require.NoError(t, common.Unmarshal(envelope.Data, &count))
			want := len(tc.Deleted)
			if index > 0 {
				want = 0
			}
			require.Equal(t, want, count)
		}
		if tc.Group == "invalid" && wantSuccess {
			var count int
			require.NoError(t, common.Unmarshal(envelope.Data, &count))
			require.Equal(t, len(tc.Deleted), count)
		}
		if tc.Group == "create" && (tc.Success || strings.HasPrefix(tc.Setup, "insert_fail_")) {
			var keys []string
			require.NoError(t, common.Unmarshal(envelope.Data, &keys))
			require.Len(t, keys, tc.Created)
			if tc.Created == 0 {
				require.Equal(t, "null", string(envelope.Data))
			}
			for i, key := range keys {
				require.Equal(t, codes[len(f.before)+i].Key, key)
			}
		}
		if tc.Group == "get" && tc.Success {
			var row model.Redemption
			require.NoError(t, common.Unmarshal(envelope.Data, &row))
			require.Equal(t, f.before[0], row)
		}
		state := snapshotHTTP(response)
		for i := range codes {
			if codes[i].Id > 22 {
				marker := fmt.Sprintf("<generated-key-%03d>", i-len(f.before)+1)
				state.Body = strings.ReplaceAll(state.Body, codes[i].Key, marker)
				codes[i].Key = marker
				state.Body = strings.ReplaceAll(state.Body, `"created_time":`+strconv.FormatInt(codes[i].CreatedTime, 10), `"created_time":1`)
				codes[i].CreatedTime = 1
			}
			if codes[i].Id == tc.RedeemedID {
				codes[i].RedeemedTime = 1
			}
			if slices.Contains(tc.Deleted, codes[i].Id) {
				codes[i].DeletedAt.Time = time.Unix(1, 0).UTC()
			}
		}
		logs := readLogs(t, f, tc, before, response.Header().Get(common.RequestIdKey))
		events := readEvents(t, f, tc, index, wantSuccess, before, response.Header().Get(common.RequestIdKey))
		codeJSON, err := common.Marshal(codes)
		require.NoError(t, err)
		steps = append(steps, stepSnapshot{state, codeJSON, wallets, logs, events})
		var count int64
		require.NoError(t, f.env.DB.Model(&model.Log{}).Count(&count).Error)
		require.EqualValues(t, 1, count, "usage must use LOG_DB")
		require.NoError(t, f.env.DB.Model(&model.AuditLog{}).Count(&count).Error)
		require.EqualValues(t, 1, count, "audit must use LOG_DB")
		require.NoError(t, f.env.DB.Model(&model.TopUp{}).Count(&count).Error)
		require.Zero(t, count, "redeeming does not create payment orders")
	}
	if *preflight {
		encoded, err := common.Marshal(steps)
		require.NoError(t, err)
		fmt.Printf("REDEMPTION_SNAPSHOT %s %x\n", tc.Name, sha256.Sum256(encoded))
		return
	}
	contracttest.GoldenValue(t, "redemption_"+tc.Name, steps)
}

func assertCodeEffects(t *testing.T, f fixture, tc contractCase, rows []model.Redemption, index int, started int64) {
	t.Helper()
	require.Len(t, rows, len(f.before)+tc.Created)
	expected := slices.Clone(f.before)
	for i := range expected {
		row := &expected[i]
		if slices.Contains(tc.Deleted, row.Id) {
			require.True(t, rows[i].DeletedAt.Valid)
			if index == 0 {
				require.GreaterOrEqual(t, rows[i].DeletedAt.Time.Unix(), started)
				require.LessOrEqual(t, rows[i].DeletedAt.Time.Unix(), time.Now().Unix())
			}
			row.DeletedAt = rows[i].DeletedAt
		}
		if row.Id == 11 && tc.Change != "" {
			switch tc.Change {
			case "changed":
				row.Name, row.Quota = "changed", 700
			case "empty_name":
				row.Name = ""
			case "long_name":
				row.Name = strings.Repeat("x", 21)
			case "future":
				row.Name, row.Quota, row.ExpiredTime = "changed", 700, 4102444800
			case "quota_max":
				row.Name, row.Quota = "changed", 9007199254740991
			case "disabled":
				row.Status = 2
			case "used":
				row.Status = 3
			case "status_zero":
				row.Status = 0
			case "status_unknown":
				row.Status = 99
			default:
				t.Fatalf("unknown expected update %q", tc.Change)
			}
		}
		if row.Id == tc.RedeemedID {
			actorID := 1
			if strings.HasPrefix(tc.Actor, "admin") {
				actorID = 2
			}
			if strings.HasPrefix(tc.Actor, "root") {
				actorID = 3
			}
			row.Status, row.UsedUserId = 3, actorID
			require.Positive(t, rows[i].RedeemedTime)
			if index == 0 {
				require.GreaterOrEqual(t, rows[i].RedeemedTime, started)
			}
			require.LessOrEqual(t, rows[i].RedeemedTime, time.Now().Unix())
			row.RedeemedTime = rows[i].RedeemedTime
		}
	}
	require.Equal(t, expected, rows[:len(f.before)], "unrequested code fields and rollback state")
	var requested struct {
		Name        string `json:"name"`
		Quota       int    `json:"quota"`
		ExpiredTime int64  `json:"expired_time"`
	}
	if tc.Created > 0 {
		require.NoError(t, common.UnmarshalJsonStr(tc.Body, &requested))
	}
	keys := map[string]bool{}
	for i, row := range rows[len(f.before):] {
		require.Regexp(t, `^[a-f0-9]{32}$`, row.Key)
		require.False(t, keys[row.Key])
		keys[row.Key] = true
		require.GreaterOrEqual(t, row.CreatedTime, started)
		require.LessOrEqual(t, row.CreatedTime, time.Now().Unix())
		issuer := 2
		if strings.HasPrefix(tc.Actor, "root") {
			issuer = 3
		}
		require.Equal(t, model.Redemption{Id: 23 + i, UserId: issuer, Key: row.Key, Name: requested.Name, Quota: requested.Quota,
			Status: 1, CreatedTime: row.CreatedTime, ExpiredTime: requested.ExpiredTime}, row)
	}
}

func readLogs(t *testing.T, f fixture, tc contractCase, started int64, httpID string) json.RawMessage {
	t.Helper()
	rows := []model.Log{}
	require.NoError(t, f.log.Order("id").Find(&rows).Error)
	want := 0
	if tc.Credit > 0 && tc.Setup != "log_failure" {
		want = 1
	}
	require.Len(t, rows, want)
	for i := range rows {
		row := &rows[i]
		id := 1
		username := "contract-user"
		if strings.HasPrefix(tc.Actor, "admin") {
			id, username = 2, "contract-admin"
		}
		if strings.HasPrefix(tc.Actor, "root") {
			id, username = 3, "contract-root"
		}
		require.Equal(t, id, row.UserId)
		require.Equal(t, username, row.Username)
		require.Equal(t, 1, row.Type)
		require.Zero(t, row.Quota)
		require.Zero(t, row.PromptTokens)
		require.Zero(t, row.CompletionTokens)
		require.Empty(t, row.Other)
		require.NotEmpty(t, row.Content)
		require.Contains(t, row.Content, "11")
		require.NotEmpty(t, row.RequestId)
		require.NotEqual(t, httpID, row.RequestId)
		require.Positive(t, row.CreatedAt)
		require.LessOrEqual(t, row.CreatedAt, time.Now().Unix())
		row.CreatedAt = 1
		row.RequestId = "<usage-request>"
	}
	encoded, err := common.Marshal(rows)
	require.NoError(t, err)
	return encoded
}

func readEvents(t *testing.T, f fixture, tc contractCase, index int, success bool, started int64, httpID string) json.RawMessage {
	t.Helper()
	rows := []model.AuditLog{}
	require.NoError(t, f.log.Order("id").Find(&rows).Error)
	pat := strings.HasSuffix(tc.Actor, "_pat")
	operation := (strings.HasPrefix(tc.Actor, "admin") || strings.HasPrefix(tc.Actor, "root")) && tc.Method != "GET" && tc.Group != "redeem"
	perRequest := 0
	if pat {
		perRequest++
	}
	if operation {
		perRequest++
	}
	require.Len(t, rows, perRequest*(index+1))
	current := rows[len(rows)-perRequest:]
	operationCount, patCount := 0, 0
	for _, row := range current {
		require.Equal(t, httpID, row.RequestId)
		require.Equal(t, tc.Status, row.Status)
		require.Equal(t, success, row.Success)
		require.GreaterOrEqual(t, row.CreatedAt, started)
		require.LessOrEqual(t, row.CreatedAt, time.Now().Unix())
		if row.Category == "access_token" {
			patCount++
			require.NotEmpty(t, row.TokenRef)
			continue
		}
		operationCount++
		require.Equal(t, "operation", row.Category)
		action := map[string]string{"update": "redemption.update", "delete": "redemption.delete", "invalid": "redemption.delete_invalid",
			"batch": "redemption.delete_batch", "create": "generic"}[tc.Group]
		if tc.Group == "create" && success {
			action = "redemption.create"
		}
		require.Equal(t, action, row.Action)
		if tc.Group == "batch" && success {
			require.NotNil(t, row.Other.Op)
			encoded, err := common.Marshal(row.Other.Op.Params)
			require.NoError(t, err)
			var params struct {
				Count int   `json:"count"`
				Total int   `json:"total"`
				IDs   []int `json:"requested_redemption_ids"`
			}
			require.NoError(t, common.Unmarshal(encoded, &params))
			var body struct {
				IDs []int `json:"ids"`
			}
			require.NoError(t, common.UnmarshalJsonStr(tc.Body, &body))
			want := len(tc.Deleted)
			if index > 0 {
				want = 0
			}
			require.Equal(t, want, params.Count)
			require.Equal(t, len(body.IDs), params.Total)
			require.Equal(t, body.IDs, params.IDs)
		}
	}
	if pat {
		require.Equal(t, 1, patCount)
	}
	if operation {
		require.Equal(t, 1, operationCount)
	}
	ids := map[string]bool{}
	for i := range rows {
		row := &rows[i]
		require.NotEmpty(t, row.EventId)
		require.False(t, ids[row.EventId])
		ids[row.EventId] = true
		row.CreatedAt = 1
		row.RequestId = fmt.Sprintf("<http-request-%d>", i/perRequest+1)
		row.EventId = fmt.Sprintf("<event-%d>", i+1)
	}
	encoded, err := common.Marshal(rows)
	require.NoError(t, err)
	for _, bearer := range f.bearers {
		if bearer != "" {
			assert.NotContains(t, string(encoded), bearer)
		}
	}
	for _, row := range f.before {
		assert.NotContains(t, string(encoded), row.Key)
	}
	return encoded
}

// Locale evidence is a separate immutable origin oracle, never a candidate golden rewrite.
func writeLocaleEvidence(t *testing.T, destination string) {
	f := newFixture(t)
	evidence := map[string]httpSnapshot{}
	for _, locale := range []string{"default", "en", "ru", "zh-CN", "zh-TW", "unknown"} {
		for _, row := range []struct{ name, method, path, body string }{
			{"get_id0", "GET", "/api/redemption/0", ""},
			{"update_id0", "PUT", "/api/redemption/", `{"id":0,"quota":0}`},
			{"delete_bad_id", "DELETE", "/api/redemption/bad", ""},
			{"create_quota0", "POST", "/api/redemption/", `{"name":"new","count":1,"quota":0}`},
			{"create_wallet_max", "POST", "/api/redemption/", `{"name":"new","count":1,"quota":9007199254740992}`},
			{"create_name", "POST", "/api/redemption/", `{"name":"","count":1,"quota":500}`},
			{"create_count", "POST", "/api/redemption/", `{"name":"new","count":0,"quota":500}`},
			{"create_expiry", "POST", "/api/redemption/", `{"name":"new","count":1,"quota":500,"expired_time":1}`},
			{"redeem_empty", "POST", "/api/user/topup", `{"key":""}`},
			{"batch_empty", "POST", "/api/redemption/batch", `{"ids":[]}`},
		} {
			r := httptest.NewRequest(row.method, row.path, strings.NewReader(row.body))
			r.Header.Set("Authorization", "Bearer "+f.env.Root)
			r.Header.Set("Content-Type", "application/json")
			if locale != "default" {
				lang := locale
				if locale == "unknown" {
					lang = "xx-YY"
				}
				r.Header.Set("Accept-Language", lang)
			}
			response := f.env.Serve(r)
			require.Equal(t, 200, response.Code)
			var body struct {
				Success bool `json:"success"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
			require.False(t, body.Success)
			evidence[locale+" "+row.name] = snapshotHTTP(response)
		}
	}
	encoded, err := common.Marshal(evidence)
	require.NoError(t, err)
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	require.NoError(t, err, "locale evidence is write-once")
	_, err = file.Write(append(encoded, '\n'))
	require.NoError(t, err)
	require.NoError(t, file.Close())
}

// Blocking the first JSON read proves the per-user lock is held before binding starts.
type admissionBody struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	reader  io.Reader
}

func (body *admissionBody) Read(data []byte) (int, error) {
	body.once.Do(func() { close(body.entered); <-body.release })
	return body.reader.Read(data)
}

func admissionRejection(t *testing.T, f fixture, request *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	body := &admissionBody{entered: make(chan struct{}), release: make(chan struct{}), reader: strings.NewReader(`{`)}
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(body.release) }) })
	first := httptest.NewRequest("POST", "/api/user/topup", body)
	first.Header.Set("Authorization", "Bearer "+f.env.User)
	first.Header.Set("Content-Type", "application/json")
	first.RemoteAddr = "192.0.2.9:3456"
	completed := make(chan *httptest.ResponseRecorder, 1)
	go func() { completed <- f.env.Serve(first) }()
	select {
	case <-body.entered:
	case <-t.Context().Done():
		t.Fatal("first request never entered JSON binding")
	}
	response := f.env.Serve(request)
	release.Do(func() { close(body.release) })
	select {
	case firstResponse := <-completed:
		require.Equal(t, 200, firstResponse.Code)
		var result struct {
			Success bool `json:"success"`
		}
		require.NoError(t, common.Unmarshal(firstResponse.Body.Bytes(), &result))
		require.False(t, result.Success)
	case <-t.Context().Done():
		t.Fatal("first request did not finish")
	}
	return response
}
