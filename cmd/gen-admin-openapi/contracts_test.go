package main

import (
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"testing"

	adminrouter "github.com/QuantumNous/new-api/router"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdminRouteDiscoveryMatchesRegisteredRuntimeRoutes(t *testing.T) {
	routes = nil
	require.NoError(t, parseRoutes("../../router"))
	dedupeRoutes()
	discovered := map[string]bool{}
	for _, route := range routes {
		if strings.HasPrefix(route.Path, "/api/") {
			discovered[route.Method+" "+route.Path] = true
		}
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	adminrouter.SetApiRouter(engine)
	registered := map[string]bool{}
	for _, route := range engine.Routes() {
		path := route.Path
		for _, segment := range strings.Split(path, "/") {
			if strings.HasPrefix(segment, ":") {
				path = strings.ReplaceAll(path, segment, "{"+segment[1:]+"}")
			}
		}
		registered[route.Method+" "+path] = true
	}
	assert.Equal(t, registered, discovered)
	assert.NotContains(t, discovered, "POST /api/option/pricing/adjust")
	assert.NotContains(t, discovered, "GET /api/option/pricing/models/{channel_type}")
	for _, operation := range []string{"GET /api/option/model_pricing", "PATCH /api/option/model_pricing", "POST /api/channel/fetch_models", "POST /api/user/group/batch"} {
		assert.Contains(t, discovered, operation)
	}
}

func TestTokenAutoGroupsSchemaMatchesJSONWireShape(t *testing.T) {
	t.Chdir("../..")
	require.NoError(t, bootstrap())
	for _, typeName := range []string{"tokenRequest", "tokenResponse"} {
		t.Run(typeName, func(t *testing.T) {
			require.Contains(t, modelTypes, typeName)
			properties := structToSchema(modelTypes[typeName])["properties"].(map[string]interface{})
			assert.Equal(t, map[string]interface{}{"type": "string"}, properties["name"])
			assert.Equal(t, map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "nullable": true}, properties["auto_groups"])
		})
	}
}

func TestGeneratedAdminContractsMatchResolvedHandlers(t *testing.T) {
	t.Chdir("../..")
	require.NoError(t, bootstrap())
	paths := map[string]interface{}{}
	applyManifest(paths)
	reconcileRoutes(paths)
	defaultUntypedResponses(paths)
	enrichFromHandlers(paths)
	applyManifestBodies(paths)
	defaultUntypedResponses(paths)
	enrichErrorResponses(paths)
	enrichExplicitContracts(paths)
	components := map[string]interface{}{}
	schemas := buildSchemas()
	enrichGetAPIInitializeSchemas(schemas)
	schemas["GetApiCreateCredentialResponse"] = wrapResponse(map[string]interface{}{"$ref": "#/components/schemas/GetAPICreateCredential"})
	schemas["GetApiCreateCredential"] = structToSchema(modelTypes["GetAPICreateCredential"])
	schemas["GetAPICreateCredential"].(map[string]interface{})["required"] = []string{"user_id", "access_token"}
	normalizeGetAPIErrorSchema(schemas)
	components["schemas"] = schemas
	enrichSecurityContracts(paths, components)
	operation := func(path, method string) map[string]interface{} {
		require.Contains(t, paths, path)
		return paths[path].(map[string]interface{})[method].(map[string]interface{})
	}

	for _, custom := range []struct {
		path, method, operationID string
		statuses                  []string
	}{
		{"/api/getapi/users", "post", "provisionGetApiUser", []string{"201", "400", "401", "403", "404", "409", "503"}},
		{"/api/getapi/users/{user_id}/pat", "post", "initializeGetApiPat", []string{"200", "201", "400", "401", "403", "404", "409", "503"}},
	} {
		op := operation(custom.path, custom.method)
		assert.Equal(t, custom.operationID, op["operationId"])
		assert.Equal(t, defaultSecurity(), op["security"])
		responses := op["responses"].(map[string]interface{})
		assert.Len(t, responses, len(custom.statuses))
		for _, status := range custom.statuses {
			require.Contains(t, responses, status)
			response := responses[status].(map[string]interface{})
			expected := "#/components/schemas/GetApiErrorResponse"
			if custom.path == "/api/getapi/users" && status == "201" {
				expected = "#/components/schemas/GetApiCreateCredentialResponse"
			} else if custom.path == "/api/getapi/users/{user_id}/pat" && (status == "200" || status == "201") {
				expected = "#/components/schemas/GetApiInitializePATResponse"
			}
			assert.Equal(t, expected, extractContentSchema(response)["$ref"])
		}
	}
	for _, path := range []string{"/api/token/"} {
		responses := operation(path, "post")["responses"].(map[string]interface{})
		assert.NotContains(t, responses, "200")
		require.Contains(t, responses, "201")
		schema := responses["201"].(map[string]interface{})["content"].(map[string]interface{})["application/json"].(map[string]interface{})["schema"].(map[string]interface{})
		assert.NotEqual(t, "#/components/schemas/ApiResponse", schema["$ref"])
	}
	createSchema := components["schemas"].(map[string]interface{})["GetAPICreateCredential"].(map[string]interface{})
	assert.Equal(t, []string{"user_id", "access_token"}, createSchema["required"])
	createProperties := createSchema["properties"].(map[string]interface{})
	assert.Contains(t, createProperties, "user_id")
	assert.Contains(t, createProperties, "access_token")
	assert.NotContains(t, createProperties["access_token"].(map[string]interface{}), "writeOnly")
	initSchema := components["schemas"].(map[string]interface{})["GetAPIInitializePATResult"].(map[string]interface{})
	assert.Equal(t, []string{"user_id", "state", "outcome", "access_token"}, initSchema["required"])
	assert.NotContains(t, initSchema["properties"].(map[string]interface{})["access_token"].(map[string]interface{}), "writeOnly")
	initRequest := components["schemas"].(map[string]interface{})["GetAPIInitializePATRequest"].(map[string]interface{})
	assert.Equal(t, []string{"expected_username"}, initRequest["required"])
	assert.Equal(t, map[string]interface{}{"type": "boolean", "default": false}, initRequest["properties"].(map[string]interface{})["apply"])
	assert.NotContains(t, initRequest["properties"], "UserID")
	errorSchema := components["schemas"].(map[string]interface{})["GetApiErrorResponse"].(map[string]interface{})
	errorCode := errorSchema["properties"].(map[string]interface{})["code"].(map[string]interface{})
	assert.Equal(t, getAPIErrorCodes, errorCode["enum"])
	userResponses := operation("/api/user/", "post")["responses"].(map[string]interface{})
	assert.NotContains(t, userResponses, "201")
	require.Contains(t, userResponses, "200")
	userSchema := extractContentSchema(userResponses["200"].(map[string]interface{}))
	userProperties := userSchema["properties"].(map[string]interface{})
	assert.Contains(t, userProperties, "success")
	assert.Contains(t, userProperties, "message")
	assert.NotContains(t, userProperties, "data")
	enrichExplicitContracts(paths)
	pricingParameters := operation("/api/option/model_pricing", "get")["parameters"].([]any)
	require.Len(t, pricingParameters, 1)
	assert.Equal(t, map[string]any{
		"name":     "model",
		"in":       "query",
		"required": false,
		"style":    "form",
		"explode":  true,
		"schema": map[string]any{
			"type":  "array",
			"items": map[string]any{"type": "string"},
		},
	}, pricingParameters[0])
	pricing := operation("/api/option/model_pricing", "patch")
	pricingResponse := pricing["responses"].(map[string]interface{})["200"].(map[string]interface{})
	pricingSchema := extractContentSchema(pricingResponse)
	envelope := pricingSchema["properties"].(map[string]interface{})
	assert.Equal(t, map[string]interface{}{"type": "boolean"}, envelope["success"])
	data := envelope["data"].(map[string]interface{})["properties"].(map[string]interface{})
	assert.Equal(t, map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}}, data["updated_models"])
	effective := operation("/api/user/effective-pricing", "get")
	effectiveSchema := extractContentSchema(effective["responses"].(map[string]interface{})["200"].(map[string]interface{}))
	assert.Equal(t, "#/components/schemas/EffectivePricingResponse", effectiveSchema["$ref"])
	effectiveByGroup := operation("/api/pricing/effective", "get")
	effectiveByGroupSchema := extractContentSchema(effectiveByGroup["responses"].(map[string]interface{})["200"].(map[string]interface{}))
	assert.Equal(t, "#/components/schemas/EffectivePricingResponse", effectiveByGroupSchema["$ref"])
	assert.Equal(t, defaultSecurity(), effectiveByGroup["security"])
	assert.Contains(t, effectiveByGroup["description"], "Requires root dashboard access")
	groupParameters := effectiveByGroup["parameters"].([]interface{})
	require.Len(t, groupParameters, 1)
	groupParameter := groupParameters[0].(map[string]interface{})
	assert.Equal(t, "group", groupParameter["name"])
	assert.Equal(t, false, groupParameter["required"])
	assert.Equal(t, map[string]interface{}{"type": "string", "default": "default"}, groupParameter["schema"])
	for _, status := range []string{"200", "400", "401", "403", "503"} {
		assert.Contains(t, effectiveByGroup["responses"], status)
	}
	effectiveData := components["schemas"].(map[string]interface{})["EffectivePricingData"].(map[string]interface{})
	effectiveProps := effectiveData["properties"].(map[string]interface{})
	assert.Equal(t, []string{"v1"}, effectiveProps["schema_version"].(map[string]interface{})["enum"])
	assert.Equal(t, []string{"RUB"}, effectiveProps["currency"].(map[string]interface{})["enum"])
	assert.Equal(t, []string{"current_tariff"}, effectiveProps["price_kind"].(map[string]interface{})["enum"])
	groupPrice := components["schemas"].(map[string]interface{})["EffectiveGroupPricing"].(map[string]interface{})
	groupProps := groupPrice["properties"].(map[string]interface{})
	assert.Equal(t, []string{"unit_prices", "formula", "unavailable"}, groupProps["status"].(map[string]interface{})["enum"])
	assert.Equal(t, []string{"token", "task", "per_call"}, groupProps["billing_surface"].(map[string]interface{})["enum"])
	assert.Contains(t, groupPrice["required"], "is_free")
	assert.Contains(t, groupPrice["required"], "tiers")
	assert.Equal(t, "#/components/schemas/EffectivePricingTier", groupProps["tiers"].(map[string]interface{})["items"].(map[string]interface{})["$ref"])
	assert.Equal(t, map[string]interface{}{"type": "boolean"}, groupProps["is_free"])
	assert.Equal(t, []string{"user_group"}, effectiveProps["price_scope"].(map[string]interface{})["enum"])
	tier := components["schemas"].(map[string]interface{})["EffectivePricingTier"].(map[string]interface{})
	tierProps := tier["properties"].(map[string]interface{})
	assert.Contains(t, tier["required"], "unit_prices")
	assert.Equal(t, "#/components/schemas/EffectivePricingTierCondition", tierProps["condition"].(map[string]interface{})["$ref"])
	assert.Equal(t, "#/components/schemas/EffectiveUnitPrice", tierProps["unit_prices"].(map[string]interface{})["items"].(map[string]interface{})["$ref"])
	condition := components["schemas"].(map[string]interface{})["EffectivePricingTierCondition"].(map[string]interface{})
	assert.Equal(t, "#/components/schemas/EffectivePricingTimeWindow", condition["properties"].(map[string]interface{})["time_windows"].(map[string]interface{})["items"].(map[string]interface{})["$ref"])
	sync := operation("/api/models/sync_upstream", "post")
	body := sync["requestBody"].(map[string]interface{})
	assert.Equal(t, true, body["required"])
	schema := body["content"].(map[string]interface{})["application/json"].(map[string]interface{})["schema"].(map[string]interface{})
	assert.ElementsMatch(t, []string{"source_version", "selections"}, schema["required"])
	properties := schema["properties"].(map[string]interface{})
	assert.Equal(t, 1, properties["selections"].(map[string]interface{})["minItems"])
	assert.Equal(t, "#/components/schemas/MetadataSyncSelection", properties["selections"].(map[string]interface{})["items"].(map[string]interface{})["$ref"])
	key := operation("/api/channel/{id}/key", "post")
	assert.Equal(t, []interface{}{map[string]interface{}{"DashboardSession": []interface{}{}, "SecurityProof": []interface{}{}}}, key["security"])
	proof := key["x-security-proof"].(map[string]interface{})
	assert.Equal(t, []string{"channel.key.read"}, proof["scopes"])
	assert.Equal(t, true, proof["single_use"])
	assert.NotContains(t, operation("/api/token/{id}/key", "post"), "x-security-proof")
	for _, path := range []string{"/api/log/self", "/api/data/self"} {
		op := operation(path, "get")
		var tokenID map[string]interface{}
		for _, parameter := range op["parameters"].([]interface{}) {
			entry := parameter.(map[string]interface{})
			if entry["name"] == "token_id" && entry["in"] == "query" {
				tokenID = entry
				break
			}
		}
		require.NotNil(t, tokenID)
		assert.Equal(t, false, tokenID["required"])
		assert.Equal(t, map[string]interface{}{"type": "integer", "minimum": 1}, tokenID["schema"])
	}
	for _, bucketed := range []struct{ path, projection string }{
		{"/api/data/", "user_id is 0 and username is empty in every row"},
		{"/api/data/self", "user_id, username, model_name, created_at, count, quota, token_used"},
		{"/api/data/users", "Rows are grouped per user and report username, created_at, count, quota, token_used."},
	} {
		op := operation(bucketed.path, "get")
		var granularity map[string]interface{}
		for _, parameter := range op["parameters"].([]interface{}) {
			entry := parameter.(map[string]interface{})
			if entry["name"] == "granularity" && entry["in"] == "query" {
				granularity = entry
				break
			}
		}
		require.NotNil(t, granularity)
		assert.Equal(t, false, granularity["required"])
		assert.Equal(t, map[string]interface{}{"type": "string", "enum": []string{"hour", "day", "week", "month"}}, granularity["schema"])
		assert.Contains(t, granularity["description"], bucketed.projection)
		assert.Contains(t, granularity["description"], "created_at >= 0")

		response := op["responses"].(map[string]interface{})["400"].(map[string]interface{})
		assert.Contains(t, response["x-error-codes"], "invalid_granularity")
		examples := response["content"].(map[string]interface{})["application/json"].(map[string]interface{})["examples"].(map[string]interface{})
		for _, code := range response["x-error-codes"].([]interface{}) {
			assert.Contains(t, examples, code)
		}
	}
	dataResponse := operation("/api/data/", "get")["responses"].(map[string]interface{})["400"].(map[string]interface{})
	assert.Equal(t, []interface{}{"invalid_params", "invalid_granularity", "month_span_exceeded"}, dataResponse["x-error-codes"])
	selfResponse := operation("/api/data/self", "get")["responses"].(map[string]interface{})["400"].(map[string]interface{})
	assert.Equal(t, []interface{}{"invalid_token_id", "time_span_exceeded", "invalid_granularity"}, selfResponse["x-error-codes"])
	usersResponse := operation("/api/data/users", "get")["responses"].(map[string]interface{})["400"].(map[string]interface{})
	assert.Equal(t, []interface{}{"invalid_params", "invalid_granularity", "month_span_exceeded"}, usersResponse["x-error-codes"])
}

func TestAllGeneratedSecurityReferencesAndAnonymousExceptions(t *testing.T) {
	t.Chdir("../..")
	require.NoError(t, bootstrap())
	paths := map[string]interface{}{}
	reconcileRoutes(paths)
	components := map[string]interface{}{}
	enrichSecurityContracts(paths, components)
	spec := map[string]interface{}{"paths": paths, "components": components, "security": defaultSecurity()}
	require.NoError(t, validateSecurityRequirements(spec))
	for _, tc := range []struct {
		path, method, scheme string
		optional             bool
	}{
		{"/api/user/login", "post", "", false}, {"/api/user/register", "post", "", false}, {"/api/status", "get", "", false},
		{"/api/user/", "post", "AccessToken1", false}, {"/api/channel/", "get", "AccessToken1", false},
		{"/api/user/auth/refresh", "post", "RefreshCookieAuth", false}, {"/api/user/sessions", "get", "DashboardSession", false},
		{"/api/pricing", "get", "AccessToken1", true}, {"/api/oauth/state", "post", "AccessToken1", true},
		{"/api/usage/token/", "get", "RelayToken", false},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			require.Contains(t, paths, tc.path)
			op := paths[tc.path].(map[string]interface{})[tc.method].(map[string]interface{})
			requirements := op["security"].([]interface{})
			if tc.scheme == "" {
				assert.Empty(t, requirements)
				return
			}
			assert.Contains(t, requirements, map[string]interface{}{tc.scheme: []interface{}{}})
			if tc.optional {
				assert.Contains(t, requirements, map[string]interface{}{})
			} else {
				assert.NotContains(t, requirements, map[string]interface{}{})
			}
		})
	}
	spec["security"] = []interface{}{map[string]interface{}{"missing-scheme": []interface{}{}}}
	require.ErrorContains(t, validateSecurityRequirements(spec), "undefined security scheme")
}

func TestLoginAndMetadataSelectionWireContracts(t *testing.T) {
	t.Chdir("../..")
	require.NoError(t, bootstrap())
	schemas := buildSchemas()
	enrichLoginSchemas(schemas)
	// Selection may not have been referenced by a manifest-only bootstrap.
	schemas["MetadataSyncSelection"] = structToSchema(modelTypes["MetadataSyncSelection"])
	enrichMetadataSelectionSchema(schemas)
	session := schemas["LoginSessionData"].(map[string]interface{})
	assert.ElementsMatch(t, []string{"access_token", "token_type", "access_expires_at", "session", "user"}, session["required"])
	properties := session["properties"].(map[string]interface{})
	assert.Equal(t, map[string]interface{}{"type": "string"}, properties["access_token"])
	assert.NotContains(t, properties, "refresh_token")
	assert.NotContains(t, schemas["LoginUser"].(map[string]interface{})["properties"], "password")
	challenge := schemas["LoginChallenge"].(map[string]interface{})
	assert.ElementsMatch(t, []string{"require_verification", "flow_token", "expires_at", "methods"}, challenge["required"])
	assert.Equal(t, []bool{true}, challenge["properties"].(map[string]interface{})["require_verification"].(map[string]interface{})["enum"])
	response := schemas["LoginResponse"].(map[string]interface{})["properties"].(map[string]interface{})["data"].(map[string]interface{})
	assert.Equal(t, []interface{}{map[string]interface{}{"$ref": "#/components/schemas/LoginSessionData"}, map[string]interface{}{"$ref": "#/components/schemas/LoginChallenge"}}, response["oneOf"])
	selection := schemas["MetadataSyncSelection"].(map[string]interface{})
	assert.ElementsMatch(t, []string{"model_name", "record_version"}, selection["required"])
	selectionProperties := selection["properties"].(map[string]interface{})
	for _, name := range []string{"model_name", "record_version"} {
		assert.Equal(t, 1, selectionProperties[name].(map[string]interface{})["minLength"])
	}
	variants := selection["oneOf"].([]interface{})
	create := variants[0].(map[string]interface{})
	update := variants[1].(map[string]interface{})
	assert.Equal(t, []string{"create"}, create["required"])
	assert.Equal(t, []bool{true}, create["properties"].(map[string]interface{})["create"].(map[string]interface{})["enum"])
	assert.Equal(t, []string{"fields"}, update["required"])
	assert.Equal(t, 1, update["properties"].(map[string]interface{})["fields"].(map[string]interface{})["minItems"])
	assert.Equal(t, []bool{false}, update["properties"].(map[string]interface{})["create"].(map[string]interface{})["enum"])
}

func TestModelPricingAndRatioSyncSuccessContracts(t *testing.T) {
	t.Chdir("../..")
	require.NoError(t, bootstrap())
	paths := map[string]any{}
	applyManifest(paths)
	reconcileRoutes(paths)
	defaultUntypedResponses(paths)
	enrichFromHandlers(paths)
	applyManifestBodies(paths)
	defaultUntypedResponses(paths)
	enrichExplicitContracts(paths)
	pricing := paths["/api/option/model_pricing"].(map[string]any)["patch"].(map[string]any)
	body := pricing["requestBody"].(map[string]any)
	assert.Equal(t, true, body["required"])
	request := body["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	assert.Equal(t, []string{"changes"}, request["required"])
	assert.Equal(t, 1, request["properties"].(map[string]any)["changes"].(map[string]any)["minItems"])
	sync := paths["/api/ratio_sync/fetch"].(map[string]any)["post"].(map[string]any)
	response := sync["responses"].(map[string]any)["200"].(map[string]any)
	assert.Equal(t, map[string]any{"$ref": "#/components/schemas/RatioSyncResponse"}, response["content"].(map[string]any)["application/json"].(map[string]any)["schema"])
	schemas := buildSchemas()
	data := schemas["RatioSyncData"].(map[string]any)
	assert.Equal(t, []string{"differences", "prices", "test_results"}, data["required"])
	props := data["properties"].(map[string]any)
	assert.Equal(t, map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/TestResult"}}, props["test_results"])
	assert.Equal(t, map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "object", "additionalProperties": map[string]any{"$ref": "#/components/schemas/DifferenceItem"}}}, props["differences"])
	prices := props["prices"].(map[string]any)["additionalProperties"].(map[string]any)
	assert.Equal(t, []string{"current", "upstreams"}, prices["required"])
	assert.Equal(t, map[string]any{"$ref": "#/components/schemas/UpstreamPricingValues"}, prices["properties"].(map[string]any)["current"])
	assert.Equal(t, []string{"name", "status"}, schemas["TestResult"].(map[string]any)["required"])
	difference := schemas["DifferenceItem"].(map[string]any)
	assert.Equal(t, []string{"current", "upstreams", "confidence"}, difference["required"])
	differenceProperties := difference["properties"].(map[string]any)
	for location, value := range map[string]any{
		"current":                        differenceProperties["current"],
		"upstreams.additionalProperties": differenceProperties["upstreams"].(map[string]any)["additionalProperties"],
	} {
		t.Run(location, func(t *testing.T) {
			scalar, ok := value.(map[string]any)
			require.True(t, ok)
			require.Len(t, scalar, 1)
			assert.NotContains(t, scalar, "type")
			assert.NotContains(t, scalar, "nullable")
			branches, ok := scalar["oneOf"].([]any)
			require.True(t, ok)
			require.Len(t, branches, 2)
			primitiveMatches := map[string]int{}
			for _, value := range branches {
				branch, ok := value.(map[string]any)
				require.True(t, ok)
				primitive, ok := branch["type"].(string)
				require.True(t, ok)
				require.Contains(t, []string{"number", "string"}, primitive)
				for keyword := range branch {
					require.Contains(t, []string{"type", "nullable"}, keyword)
				}
				primitiveMatches[primitive]++
				if primitive == "number" {
					require.Equal(t, true, branch["nullable"])
					primitiveMatches["null"]++
				} else {
					assert.NotContains(t, branch, "nullable")
				}
			}
			assert.Len(t, primitiveMatches, 3)
			for _, primitive := range []string{"number", "string", "null"} {
				assert.Equal(t, 1, primitiveMatches[primitive], "%s must match exactly one branch", primitive)
			}
			for _, primitive := range []string{"boolean", "object", "array"} {
				assert.Zero(t, primitiveMatches[primitive], "%s must match no branch", primitive)
			}
		})
	}
	assert.Equal(t, []string{"name", "base_url"}, schemas["UpstreamDTO"].(map[string]any)["required"])
	assert.Equal(t, []string{"id", "name", "base_url", "status", "type"}, schemas["SyncableChannel"].(map[string]any)["required"])
	assert.Equal(t, []string{"success", "error"}, schemas["TestResult"].(map[string]any)["properties"].(map[string]any)["status"].(map[string]any)["enum"])
	assert.Equal(t, false, schemas["UpstreamPricingValues"].(map[string]any)["additionalProperties"])
	assert.NotContains(t, schemas["UpstreamDTO"].(map[string]any)["required"], "endpoint")
	assert.NotContains(t, schemas["TestResult"].(map[string]any)["required"], "error")
	assert.Nil(t, schemas["UpstreamRequest"].(map[string]any)["required"])
	assert.Equal(t, map[string]any{"description": "Any value."}, schemas["PricingValues"].(map[string]any)["additionalProperties"])
	assert.Equal(t, map[string]any{"type": "object", "additionalProperties": difference["properties"].(map[string]any)["current"]}, difference["properties"].(map[string]any)["upstreams"])

	channels := paths["/api/ratio_sync/channels"].(map[string]any)["get"].(map[string]any)["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	assert.Equal(t, []string{"success", "message", "data"}, channels["required"])
	assert.Equal(t, map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/SyncableChannel"}}, channels["properties"].(map[string]any)["data"])
}

func TestChannelPatchContractPreservesReadAndOtherOperations(t *testing.T) {
	t.Chdir("../..")
	require.NoError(t, bootstrap())
	paths := map[string]any{}
	applyManifest(paths)
	reconcileRoutes(paths)
	defaultUntypedResponses(paths)
	enrichFromHandlers(paths)
	applyManifestBodies(paths)
	defaultUntypedResponses(paths)
	enrichErrorResponses(paths)
	enrichExplicitContracts(paths)
	put := paths["/api/channel/"].(map[string]any)["put"].(map[string]any)
	originalBody := put["requestBody"]
	before, err := common.Marshal(paths)
	require.NoError(t, err)
	allRoutes := routes
	routes = nil
	for _, route := range allRoutes {
		if route.HandlerName == "UpdateChannel" {
			routes = append(routes, route)
		}
	}
	enrichExplicitContracts(paths)
	routes = allRoutes
	patchBody := put["requestBody"]
	put["requestBody"] = originalBody
	after, err := common.Marshal(paths)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after))
	put["requestBody"] = patchBody

	channel := structToSchema(modelTypes["Channel"])
	readProperties := channel["properties"].(map[string]any)
	body := put["requestBody"].(map[string]any)
	assert.Equal(t, true, body["required"])
	schema := extractContentSchema(body)
	assert.NotContains(t, schema, "$ref")
	assert.NotContains(t, schema, "required")
	properties := schema["properties"].(map[string]any)
	assert.Len(t, properties, len(readProperties)+1)
	for name, property := range readProperties {
		if name == "status" {
			assert.NotContains(t, properties, name)
			continue
		}
		assert.Equal(t, property, properties[name], name)
	}
	for _, name := range []string{"id", "name", "key", "setting", "settings"} {
		assert.Contains(t, properties, name)
	}
	assert.Equal(t, map[string]any{"type": "string", "nullable": true, "enum": []any{"append", "replace", nil}}, properties["key_mode"])
	assert.Equal(t, map[string]any{"type": "string", "nullable": true, "enum": []any{"random", "polling", nil}}, properties["multi_key_mode"])
	assert.Contains(t, readProperties, "status")
	assert.NotContains(t, readProperties, "key_mode")
	assert.NotContains(t, readProperties, "multi_key_mode")
	response := put["responses"].(map[string]any)["200"].(map[string]any)
	assert.Equal(t, "#/components/schemas/ApiResponseOfChannel", extractContentSchema(response)["$ref"])
}

func TestGeneratedChannelTaskContracts(t *testing.T) {
	t.Chdir("../..")
	require.NoError(t, bootstrap())
	raw, err := os.ReadFile("docs/openapi/api.json")
	require.NoError(t, err)
	var spec map[string]any
	require.NoError(t, common.Unmarshal(raw, &spec))
	paths := spec["paths"].(map[string]any)
	referencedTypes = map[string]bool{}
	removeFakePaths(paths)
	clearPlaceholderBodies(paths)
	applyManifest(paths)
	reconcileRoutes(paths)
	defaultUntypedResponses(paths)
	enrichFromHandlers(paths)
	applyManifestBodies(paths)
	defaultUntypedResponses(paths)
	enrichErrorResponses(paths)
	before, err := common.Marshal(paths)
	require.NoError(t, err)
	var previous map[string]any
	require.NoError(t, common.Unmarshal(before, &previous))
	enrichExplicitContracts(paths)
	pathReferences, err := common.Marshal(paths)
	require.NoError(t, err)
	for name := range collectRefs(string(pathReferences)) {
		referencedTypes[name] = true
	}
	schemas := buildSchemas()
	components := map[string]any{"schemas": schemas}
	enrichSecurityContracts(paths, components)
	operation := func(path string) map[string]any {
		return paths[path].(map[string]any)["get"].(map[string]any)
	}
	responseSchema := func(path, status string) map[string]any {
		return extractContentSchema(operation(path)["responses"].(map[string]any)[status].(map[string]any))
	}

	t.Run("current task query and nullable actual source data", func(t *testing.T) {
		parameters := operation("/api/system-task/current")["parameters"].([]any)
		found := false
		for _, parameter := range parameters {
			entry := parameter.(map[string]any)
			if entry["name"] == "type" && entry["in"] == "query" {
				found = true
				assert.Equal(t, true, entry["required"])
				assert.Equal(t, map[string]any{"type": "string"}, entry["schema"])
			}
		}
		require.True(t, found)
		schema := responseSchema("/api/system-task/current", "200")
		assert.NotContains(t, schema, "$ref")
		assert.Equal(t, []any{"success"}, schema["required"])
		properties := schema["properties"].(map[string]any)
		assert.Len(t, properties, 3)
		assert.Equal(t, map[string]any{"type": "boolean"}, properties["success"])
		assert.Equal(t, map[string]any{"type": "string"}, properties["message"])
		data := properties["data"].(map[string]any)
		expected := structToSchema(modelTypes["SystemTaskResponse"])
		expected["nullable"] = true
		assert.Equal(t, expected, data)
		assert.NotContains(t, schemas["SystemTaskResponse"].(map[string]any), "nullable")
		assert.Equal(t, map[string]any{"$ref": "#/components/schemas/ApiResponseOfSystemTaskResponse"}, responseSchema("/api/system-task/{task_id}", "200"))
		assert.Equal(t, map[string]any{"$ref": "#/components/schemas/ApiResponseListOfSystemTaskResponse"}, responseSchema("/api/system-task/list", "200"))
		assert.Equal(t, map[string]any{"$ref": "#/components/schemas/SystemTaskResponse"}, schemas["ApiResponseOfSystemTaskResponse"].(map[string]any)["properties"].(map[string]any)["data"])
		listData := schemas["ApiResponseListOfSystemTaskResponse"].(map[string]any)["properties"].(map[string]any)["data"].(map[string]any)
		assert.Equal(t, "array", listData["type"])
		assert.NotContains(t, listData, "nullable")
		assert.Equal(t, map[string]any{"$ref": "#/components/schemas/SystemTaskResponse"}, listData["items"])
	})

	for _, status := range []string{"200", "409"} {
		t.Run("launch "+status, func(t *testing.T) {
			schema := responseSchema("/api/channel/test", status)
			assert.NotContains(t, schema, "$ref")
			assert.Equal(t, []string{"success", "message", "data"}, schema["required"])
			properties := schema["properties"].(map[string]any)
			assert.Len(t, properties, 3)
			assert.Equal(t, map[string]any{"type": "boolean", "enum": []bool{status == "200"}}, properties["success"])
			assert.Equal(t, map[string]any{"type": "string"}, properties["message"])
			expectedProperties := map[string]any{"task_id": map[string]any{"type": "string"}, "status": map[string]any{"type": "string"}}
			required := []string{"task_id", "status"}
			if status == "409" {
				expectedProperties["type"] = map[string]any{"type": "string"}
				required = append(required, "type")
			}
			assert.Equal(t, map[string]any{"type": "object", "required": required, "properties": expectedProperties}, properties["data"])
		})
	}

	for _, status := range []string{"422", "502"} {
		t.Run("single test diagnostics "+status, func(t *testing.T) {
			assert.Equal(t, map[string]any{
				"type": "object", "required": []string{"success", "code", "message", "time", "data"},
				"properties": map[string]any{
					"success": map[string]any{"type": "boolean", "enum": []bool{false}},
					"code":    map[string]any{"type": "string", "enum": []string{"channel_test_failed"}},
					"message": map[string]any{"type": "string"},
					"time":    map[string]any{"type": "number"},
					"data":    map[string]any{"$ref": "#/components/schemas/ChannelTestResponse"},
				},
			}, responseSchema("/api/channel/test/{id}", status))
		})
	}

	t.Run("unaffected contracts", func(t *testing.T) {
		assert.Equal(t, map[string]any{"$ref": "#/components/schemas/ApiResponseOfChannelTestResponse"}, responseSchema("/api/channel/test/{id}", "200"))
		assert.Equal(t, structToSchema(modelTypes["ChannelTestResponse"]), schemas["ChannelTestResponse"])
		assert.Equal(t, map[string]any{"type": "string"}, schemas["ChannelTestResponse"].(map[string]any)["properties"].(map[string]any)["error_code"])
		assert.Equal(t, map[string]any{"type": "object", "properties": map[string]any{
			"success": map[string]any{"type": "boolean"}, "code": map[string]any{"type": "string"}, "message": map[string]any{"type": "string"},
		}}, schemas["ApiErrorResponse"])
		found := false
		for _, parameter := range operation("/api/channel/search")["parameters"].([]any) {
			entry := parameter.(map[string]any)
			if entry["name"] == "type" && entry["in"] == "query" {
				found = true
				assert.Equal(t, map[string]any{"type": "integer"}, entry["schema"])
			}
		}
		require.True(t, found)
		for _, path := range []string{"/api/system-task/current", "/api/channel/test", "/api/channel/test/{id}"} {
			oldResponses := previous[path].(map[string]any)["get"].(map[string]any)["responses"].(map[string]any)
			newResponses := operation(path)["responses"].(map[string]any)
			assert.Len(t, newResponses, len(oldResponses))
			for status, response := range oldResponses {
				if path == "/api/system-task/current" && status == "200" || path == "/api/channel/test" && (status == "200" || status == "409") || path == "/api/channel/test/{id}" && (status == "422" || status == "502") {
					continue
				}
				actual, err := common.Marshal(newResponses[status])
				require.NoError(t, err)
				expected, err := common.Marshal(response)
				require.NoError(t, err)
				assert.JSONEq(t, string(expected), string(actual), path+" "+status)
			}
		}
	})
}
