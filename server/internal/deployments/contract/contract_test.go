// Package contract pins deployment management HTTP and io.net outbound contracts before migration.
package contract

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	f "github.com/QuantumNous/new-api/internal/contracttest/managementfixture"
	h "github.com/QuantumNous/new-api/internal/contracttest/managementhttp"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type deploymentCase struct {
	Name, Method, Path, Body, Actor, Variant string
	Calls                                    int
}

const createBody = `{"resource_private_name":"fixture","duration_hours":2,"gpus_per_container":1,"hardware_id":7,` +
	`"location_ids":[2],"container_config":{"replica_count":1,"env_variables":{"A":"B"},` +
	`"secret_env_variables":{"SECRET":"fixture-only"},"traffic_port":8080,"entrypoint":["run"],"args":["--serve"]},` +
	`"registry_config":{"image_url":"fixture/image:v1","registry_username":"test",` +
	`"registry_secret":"fixture-registry-secret"}}`
const priceBody = `{"location_ids":[2,1],"hardware_id":7,"gpus_per_container":2,"duration_hours":4,"replica_count":3}`

func deploymentRoutes() []deploymentCase {
	return []deploymentCase{
		{Name: "settings", Method: "GET", Path: "/settings", Calls: 0},
		{Name: "test_settings", Method: "POST", Path: "/settings/test-connection", Calls: 1},
		{Name: "test_alias", Method: "POST", Path: "/test-connection", Calls: 1},
		{Name: "list", Method: "GET", Path: "/", Calls: 1},
		{Name: "search", Method: "GET", Path: "/search", Calls: 1},
		{Name: "hardware", Method: "GET", Path: "/hardware-types", Calls: 1},
		{Name: "locations", Method: "GET", Path: "/locations", Calls: 1},
		{Name: "replicas", Method: "GET", Path: "/available-replicas?hardware_id=7", Calls: 1},
		{Name: "price", Method: "POST", Path: "/price-estimation", Body: priceBody, Calls: 1},
		{Name: "check_name", Method: "GET", Path: "/check-name?name=fixture", Calls: 1},
		{Name: "create", Method: "POST", Path: "/", Body: createBody, Calls: 1},
		{Name: "detail", Method: "GET", Path: "/dep-1", Calls: 1},
		{Name: "logs", Method: "GET", Path: "/dep-1/logs?container_id=ctr-1", Calls: 1},
		{Name: "containers", Method: "GET", Path: "/dep-1/containers", Calls: 1},
		{Name: "container", Method: "GET", Path: "/dep-1/containers/ctr-1", Calls: 1},
		{Name: "update", Method: "PUT", Path: "/dep-1", Body: `{"traffic_port":0,"env_variables":{},"args":[],"registry_secret":"fixture-secret"}`, Calls: 1},
		{Name: "rename", Method: "PUT", Path: "/dep-1/name", Body: `{"name":" next name "}`, Calls: 2},
		{Name: "extend", Method: "POST", Path: "/dep-1/extend", Body: `{"duration_hours":3}`, Calls: 1},
		{Name: "delete", Method: "DELETE", Path: "/dep-1", Calls: 1},
	}
}
func deploymentCases() []deploymentCase {
	cases := []deploymentCase{}
	for _, route := range deploymentRoutes() {
		for _, actor := range []string{"anonymous", "user", "admin", "root", "user_pat", "admin_pat", "root_pat", "key"} {
			tc := route
			tc.Name = "access_" + route.Name + "_" + actor
			tc.Actor = actor
			if actor == "anonymous" || actor == "user" || actor == "user_pat" || actor == "key" {
				tc.Calls = 0
			}
			cases = append(cases, tc)
		}
		for _, variant := range []string{"success", "disabled", "missing_key", "space_key", "api_detail", "api_raw", "api_empty", "malformed", "network"} {
			if route.Name == "settings" && !slices.Contains([]string{"success", "disabled", "missing_key", "space_key"}, variant) {
				continue
			}
			tc := route
			tc.Name = route.Name + "_" + variant
			tc.Variant = variant
			tc.Actor = "root"
			if route.Name == "settings" || ((variant == "disabled" || variant == "missing_key") && !strings.HasPrefix(route.Name, "test_")) {
				tc.Calls = 0
			}
			if variant == "missing_key" && strings.HasPrefix(route.Name, "test_") {
				tc.Calls = 0
			}
			if variant != "success" && variant != "space_key" && route.Name == "rename" && tc.Calls > 0 {
				tc.Calls = 1
			}
			cases = append(cases, tc)
		}
	}
	add := func(name, method, path, body, variant string, calls int) {
		cases = append(cases, deploymentCase{name, method, path, body, "root", variant, calls})
	}
	for i, query := range []string{"p=2&page_size=1&status= RUNNING ", "p=-2&page_size=-1",
		"p=bad&page_size=999", "ps=2", "size=2", "keyword=alpha", "keyword=MISSING", "keyword=beta&status=failed"} {
		query = strings.ReplaceAll(query, " ", "%20")
		add(fmt.Sprintf("list_query_%02d", i), "GET", "/?"+query, "", "success", 1)
		add(fmt.Sprintf("search_query_%02d", i), "GET", "/search?"+query, "", "success", 1)
	}
	for _, name := range []string{"empty", "null_data", "zero_date", "negative_remaining", "false_name", "zero_total"} {
		for _, route := range deploymentRoutes() {
			if route.Calls == 0 {
				continue
			}
			if (name == "empty" || name == "null_data") && !slices.Contains([]string{"list",
				"search", "hardware", "locations", "replicas", "logs", "containers"}, route.Name) {
				continue
			}
			if (name == "zero_date" || name == "negative_remaining") && !slices.Contains([]string{"list", "search"}, route.Name) {
				continue
			}
			if name == "false_name" && route.Name != "rename" && route.Name != "check_name" {
				continue
			}
			if name == "zero_total" && route.Name != "hardware" && route.Name != "locations" {
				continue
			}
			tc := route
			tc.Name = route.Name + "_" + name
			tc.Actor = "root"
			tc.Variant = name
			if name == "false_name" && route.Name == "rename" {
				tc.Calls = 1
			}
			cases = append(cases, tc)
		}
	}
	for i, body := range []string{"{", "null", " ", `{"api_key":" explicit-fixture-key "}`, `{"api_key":42}`} {
		add(fmt.Sprintf("connection_body_%02d", i), "POST", "/test-connection", body, "success", map[bool]int{true: 0, false: 1}[i == 0 || i == 4])
	}
	for i, body := range []string{`{}`, `{"resource_private_name":"x"}`, strings.Replace(createBody,
		`"location_ids":[2]`, `"location_ids":[]`, 1), strings.Replace(createBody, `"hardware_id":7`,
		`"hardware_id":0`, 1), strings.Replace(createBody, `"image_url":"fixture/image:v1"`,
		`"image_url":""`, 1), strings.Replace(createBody, `"gpus_per_container":1`, `"gpus_per_container":0`,
		1), strings.Replace(createBody, `"duration_hours":2`, `"duration_hours":0`, 1), strings.Replace(createBody,
		`"replica_count":1`, `"replica_count":0`, 1), "{", `{"duration_hours":"two"}`} {
		add(fmt.Sprintf("create_validation_%02d", i), "POST", "/", body, "success", 0)
	}
	for _, route := range deploymentRoutes() {
		if route.Body == "" {
			continue
		}
		for i, body := range []string{"{", `{"name":42,"duration_hours":"bad"}`} {
			tc := route
			tc.Name = fmt.Sprintf("bind_%s_%d", route.Name, i)
			tc.Actor = "root"
			tc.Body = body
			if route.Name == "update" && i == 1 {
				tc.Body = `{"traffic_port":"bad"}`
			}
			tc.Calls = 0
			cases = append(cases, tc)
		}
	}
	for i, path := range []string{"/available-replicas", "/available-replicas?hardware_id=bad",
		"/available-replicas?hardware_id=-1", "/available-replicas?hardware_id=7&gpu_count=bad",
		"/available-replicas?hardware_id=7&gpu_count=0", "/available-replicas?hardware_id=7&gpu_count=4",
		"/check-name", "/check-name?name=%20", "/dep-1/logs", "/dep-1/logs?container_id=ctr-1&limit=2000&follow=true&stream=stdout&level=info&cursor=next" +
			"&start_time=2000-01-01T00:00:00Z&end_time=2000-01-02T00:00:00Z",
		"/dep-1/logs?container_id=%20&limit=-1&start_time=bad&follow=TRUE"} {
		calls := 1
		if i < 3 || i == 6 || i == 7 || i == 8 {
			calls = 0
		}
		add(fmt.Sprintf("query_validation_%02d", i), "GET", path, "", "success", calls)
	}
	for i, body := range []string{`{"name":""}`, `{"name":"   "}`, `{}`} {
		add(fmt.Sprintf("rename_validation_%d", i), "PUT", "/dep-1/name", body, "success", 0)
	}
	add("extend_zero", "POST", "/dep-1/extend", `{"duration_hours":0}`, "success", 0)
	for i, body := range []string{`{}`, strings.Replace(priceBody, `"location_ids":[2,1]`,
		`"location_ids":[]`, 1), strings.Replace(priceBody, `"hardware_id":7`, `"hardware_id":0`,
		1), strings.Replace(priceBody, `"replica_count":3`, `"replica_count":0`, 1), strings.Replace(priceBody,
		`"duration_hours":4`, `"duration_hours":0`, 1), strings.Replace(priceBody, `"gpus_per_container":2`,
		`"gpus_per_container":0`, 1)} {
		add(fmt.Sprintf("price_validation_%02d", i), "POST", "/price-estimation", body, "success", 0)
	}
	for _, kind := range []string{"hour", "days", "weekly", "month", "weird"} {
		add("price_duration_"+kind, "POST", "/price-estimation", strings.TrimSuffix(priceBody,
			"}")+`,"duration_type":"`+kind+`","duration_qty":2,"hardware_qty":3,"currency":" eur "}`,
			"success", 1)
	}
	add("price_negative_hardware", "POST", "/price-estimation", strings.Replace(priceBody, `"hardware_id":7`, `"hardware_id":-7`, 1), "success", 1)
	add("missing_slash", "GET", "", "", "success", 0)
	add("wrong_method", "PATCH", "/dep-1", "", "success", 0)
	return cases
}
func TestDeploymentsContract(t *testing.T) {
	cases := deploymentCases()
	names := make([]string, len(cases))
	byName := map[string]deploymentCase{}
	for i, tc := range cases {
		names[i] = tc.Name
		byName[tc.Name] = tc
	}
	f.Run(t, "TestDeploymentsContract", "deployments", "testdata/golden", names, []string{"access_list_anonymous",
		"access_list_user", "settings_disabled", "connection_body_00"}, func(t *testing.T,
		name string) any {
		return runDeployment(t, byName[name])
	})
}
func runDeployment(t *testing.T, tc deploymentCase) any {
	transport := &f.Transport{Requests: []f.Request{}, Reply: func(r *http.Request, index int) (int, string, error) { return deploymentReply(r, tc.Variant) }}
	transport.EnableServer(t)
	return h.Run(t, false, func(t *testing.T, env *h.Fixture) any {
		common.OptionMap["model_deployment.ionet.enabled"] = "true"
		common.OptionMap["model_deployment.ionet.api_key"] = "fixture-io-key"
		switch tc.Variant {
		case "disabled":
			common.OptionMap["model_deployment.ionet.enabled"] = "false"
		case "missing_key":
			common.OptionMap["model_deployment.ionet.api_key"] = "   "
		case "space_key":
			common.OptionMap["model_deployment.ionet.api_key"] = " fixture-io-key "
		}

		previous := http.DefaultTransport
		http.DefaultTransport = transport
		t.Cleanup(func() { http.DefaultTransport = previous })
		response := env.Do(t, tc.Actor, tc.Method, "/api/deployments"+tc.Path, tc.Body)
		if strings.HasPrefix(tc.Name, "access_") {
			status := 200
			if tc.Actor == "anonymous" || tc.Actor == "key" {
				status = 401
			} else if tc.Actor == "user" || tc.Actor == "user_pat" {
				status = 403
			}
			assert.Equal(t, status, response.Code)
		} else if tc.Name != "missing_slash" && tc.Name != "wrong_method" {
			assert.Equal(t, 200, response.Code)
		}
		require.Len(t, transport.Requests, tc.Calls)
		for _, request := range transport.Requests {
			assert.Equal(t, "application/json", request.Header.Get("Content-Type"))
			expectedKey := "fixture-io-key"
			if tc.Variant == "space_key" && !strings.HasPrefix(tc.Name, "test_") {
				expectedKey = " fixture-io-key "
			}
			if strings.Contains(tc.Body, "explicit-fixture-key") {
				expectedKey = "explicit-fixture-key"
			}
			assert.Equal(t, expectedKey, request.Header.Get("X-Api-Key"))
		}
		if tc.Name == "rename_success" {
			require.Len(t, transport.Requests, 2)
			assert.Equal(t, "GET", transport.Requests[0].Method)
			assert.Equal(t, "PUT", transport.Requests[1].Method)
			assert.Equal(t, `{"cluster_name":"next name"}`, transport.Requests[1].Body)
		}
		return env.Snapshot(t, map[string]any{"outbound": transport.Requests})
	})
}
func deploymentReply(request *http.Request, variant string) (int, string, error) {
	switch variant {
	case "api_detail":
		return 403, `{"detail":"fixture denied"}`, nil
	case "api_raw":
		return 500, "fixture upstream raw", nil
	case "api_empty":
		return 500, "", nil
	case "malformed":
		return 200, "{", nil
	case "network":
		return 0, "", errors.New("fixture network failure")
	}
	path := request.URL.Path
	if strings.HasSuffix(path, "check_cluster_name_availability") {
		if variant == "false_name" {
			return 200, "false", nil
		}
		return 200, "true", nil
	}
	if strings.HasSuffix(path, "max-gpus-per-container") {
		if variant == "empty" || variant == "null_data" {
			return 200, `{"data":{"hardware":[],"total":0}}`, nil
		}
		total := "7"
		if variant == "zero_total" {
			total = "0"
		}
		return 200, `{"data":{"hardware":[{"hardware_id":7,"hardware_name":" GPU ","brand_name":" Brand ","available":2,` +
			`"max_gpus_per_container":4},{"hardware_id":8,"hardware_name":" ","available":0}],"total":` + total + `}}`, nil
	}
	if strings.HasSuffix(path, "/locations") {
		if variant == "empty" || variant == "null_data" {
			return 200, `{"data":{"locations":[],"total":0}}`, nil
		}
		total := "9"
		if variant == "zero_total" {
			total = "0"
		}
		return 200, `{"data":{"locations":[{"id":2,"iso2":" us ","name":"fixture","available":0}],"total":` + total + `}}`, nil
	}
	if strings.HasSuffix(path, "/available-replicas") {
		if variant == "empty" {
			return 200, `{"data":[]}`, nil
		}
		if variant == "null_data" {
			return 200, `{"data":null}`, nil
		}
		return 200, `{"data":[{"id":2,"iso2":"us","name":"fixture","available_replicas":3}]}`, nil
	}
	if strings.HasSuffix(path, "/price") {
		return 200, `{"data":{"total_cost_usdc":24,"ionet_fee":2,"currency_conversion_fee":1,"replica_count":3,"discount":0}}`, nil
	}
	if strings.Contains(path, "/log/") {
		if variant == "empty" {
			return 200, "", nil
		}
		return 200, "line one\r\n\"line two\"\n", nil
	}
	if strings.HasSuffix(path, "/update-name") {
		return 200, `{"status":"ok","message":"renamed"}`, nil
	}
	if strings.HasSuffix(path, "/deploy") || request.Method == "PATCH" || request.Method == "DELETE" {
		return 200, `{"status":"accepted","deployment_id":"dep-1"}`, nil
	}
	ctr := `{"device_id":"dev-1","container_id":"ctr-1","hardware":"GPU","brand_name":"Brand",` +
		`"created_at":"2000-01-01T00:00:00","status":" RUNNING ","public_url":"https://fixture.invalid/public",` +
		`"container_events":[{"time":"2000-01-01T00:00:01","message":"ready"}]}`
	if strings.HasSuffix(path, "/containers") {
		if variant == "empty" || variant == "null_data" {
			return 200, `{"data":null}`, nil
		}
		return 200, `{"data":{"total":9,"workers":[` + ctr + `]}}`, nil
	}
	if strings.Contains(path, "/container/") {
		return 200, ctr, nil
	}
	if strings.HasSuffix(path, "/deployments") {
		if variant == "empty" {
			return 200, `{"data":{"total":0,"deployments":[]}}`, nil
		}
		if variant == "null_data" {
			return 200, `{"data":null}`, nil
		}
		created := `"2000-01-01T00:00:00Z"`
		remaining := "65"
		if variant == "zero_date" {
			created = `"0001-01-01T00:00:00Z"`
		}
		if variant == "negative_remaining" {
			remaining = "-5"
		}
		return 200, `{"data":{"total":9,"deployments":[{"id":"dep-1","name":"Alpha","status":" RUNNING ","hardware_quantity":2,` +
			`"brand_name":"Brand","hardware_name":"GPU","compute_minutes_remaining":` + remaining + `,"created_at":` + created + `},{"id":"dep-2","name":"beta","status":"Mystery","created_at":"2000-01-02T00:00:00Z"}]}}`, nil
	}
	return 200, `{"data":{"id":"dep-1","status":"RUNNING","created_at":"2000-01-01T00:00:00","total_gpus":2,` +
		`"total_containers":1,"hardware_id":7,"hardware_name":"GPU","brand_name":"Brand","locations":null,` +
		`"container_config":{"env_variables":{},"entrypoint":[]}}}`, nil
}
