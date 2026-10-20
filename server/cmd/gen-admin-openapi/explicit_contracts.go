package main

import (
	"slices"
	"strconv"
	"strings"
)

var getAPIErrorCodes = []string{
	"GETAPI_INVALID_REQUEST",
	"AUTH_UNAUTHORIZED",
	"GETAPI_CAPABILITY_DENIED",
	"GETAPI_ACCOUNT_NOT_FOUND",
	"GETAPI_CREATE_CONFLICT",
	"GETAPI_USERNAME_MISMATCH",
	"GETAPI_TARGET_ROLE_DENIED",
	"GETAPI_CREDENTIAL_UNAVAILABLE",
}

func normalizeGetAPIErrorSchema(schemas map[string]interface{}) {
	schema, ok := schemas["GetApiErrorResponse"].(map[string]interface{})
	if !ok {
		schema = map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"success": map[string]interface{}{"type": "boolean", "enum": []bool{false}},
				"code":    map[string]interface{}{"type": "string"},
				"message": map[string]interface{}{"type": "string"},
			},
			"required": []string{"success", "code", "message"},
		}
		schemas["GetApiErrorResponse"] = schema
	}
	properties, _ := schema["properties"].(map[string]interface{})
	if properties == nil {
		properties = map[string]interface{}{}
		schema["properties"] = properties
	}
	code, ok := properties["code"].(map[string]interface{})
	if !ok {
		code = map[string]interface{}{"type": "string"}
		properties["code"] = code
	}
	code["enum"] = append([]string(nil), getAPIErrorCodes...)
}

// declareErrorCodes appends codes to an operation's x-error-codes catalog and
// mirrors them into the response examples. analyzeHandler only inspects the
// handler's own body, so codes emitted from a helper it calls stay invisible to
// enrichErrorResponses and have to be declared here instead.
func declareErrorCodes(op map[string]interface{}, status string, codes ...string) {
	responses, _ := op["responses"].(map[string]interface{})
	response, _ := responses[status].(map[string]interface{})
	if response == nil {
		return
	}
	catalog, _ := response["x-error-codes"].([]interface{})
	catalog = append([]interface{}(nil), catalog...)
	for _, code := range codes {
		if !slices.Contains(catalog, interface{}(code)) {
			catalog = append(catalog, code)
		}
	}
	response["x-error-codes"] = catalog
	content, _ := response["content"].(map[string]interface{})
	appJSON, _ := content["application/json"].(map[string]interface{})
	if appJSON == nil {
		return
	}
	examples, _ := appJSON["examples"].(map[string]interface{})
	if examples == nil {
		examples = map[string]interface{}{}
		appJSON["examples"] = examples
	}
	for _, entry := range catalog {
		code, _ := entry.(string)
		examples[code] = map[string]interface{}{
			"summary": code,
			"value": map[string]interface{}{
				"success": false,
				"code":    code,
				"message": "<localized message>",
			},
		}
	}
}

// declareGranularityParameter documents the bucketing parameter shared by the
// /api/data family. The generator discovers the query key from the AST but types
// it as a bare string, so the enum and the projection each endpoint reports have
// to be stated here.
func declareGranularityParameter(op map[string]interface{}, projection string) {
	parameters, _ := op["parameters"].([]interface{})
	schema := map[string]interface{}{"type": "string", "enum": []string{"hour", "day", "week", "month"}}
	description := "Aggregation bucket. Omitted or empty returns the unchanged legacy rows. Any value — hour included — switches to a reduced aggregate projection whose created_at is the UTC start of the bucket; weeks start on Monday and month spans are limited to 120 months per request. " + projection + " Bucket boundaries match the reference floor semantics only for created_at >= 0: the SQL % operator truncates toward zero while the reference rounds down. Stored created_at is never negative, so the two do not diverge in practice."
	found := false
	for _, parameter := range parameters {
		entry, _ := parameter.(map[string]interface{})
		if entry["name"] != "granularity" || entry["in"] != "query" {
			continue
		}
		entry["schema"] = schema
		entry["description"] = description
		found = true
	}
	if !found {
		parameters = append(parameters, map[string]interface{}{"name": "granularity", "in": "query", "required": false, "description": description, "schema": schema})
	}
	op["parameters"] = parameters
}

// AST discovery cannot infer validation expressed in ordinary control flow.
// Keep the native sync preconditions explicit rather than documenting a legacy
// no-body apply operation that the server deliberately rejects.
func enrichExplicitContracts(paths map[string]interface{}) {
	for _, route := range routes {
		path, _ := paths[route.Path].(map[string]interface{})
		op, _ := path[strings.ToLower(route.Method)].(map[string]interface{})
		h := handlers[route.HandlerName]
		if op == nil || h == nil {
			continue
		}
		if route.HandlerName == "GetAdminUserLogs" || route.HandlerName == "QueryAdminLogs" {
			properties := map[string]any{
				"type":            map[string]any{"type": "integer", "minimum": 0, "maximum": 7, "default": 0},
				"start_timestamp": map[string]any{"type": "integer", "format": "int64", "minimum": 0, "default": 0},
				"end_timestamp":   map[string]any{"type": "integer", "format": "int64", "minimum": 0, "default": 0},
				"model_name":      map[string]any{"type": "string", "description": "At most 16384 UTF-8 bytes. Existing percent wildcard semantics apply."},
				"p":               map[string]any{"type": "integer", "minimum": 1, "default": 1},
				"page_size":       map[string]any{"type": "integer", "minimum": 1, "maximum": 100, "default": 10},
			}
			idSchema := map[string]any{"type": "integer", "minimum": 1, "maximum": 2147483647}
			op["description"] = "Admin log page scoped to numeric user IDs. Inclusive nonzero time bounds; start must not exceed end. Empty IDs return an empty page. Relational order: created_at DESC, id DESC. ClickHouse order: created_at DESC, request_id DESC, with display IDs assigned after pagination; equal sort keys have unspecified order. Separate COUNT and SELECT do not provide snapshot consistency. Invalid input or exceeded limits returns HTTP 400."
			if route.HandlerName == "GetAdminUserLogs" {
				parameters := []any{map[string]any{"name": "user_id", "in": "path", "required": true, "schema": idSchema}}
				for _, name := range []string{"type", "start_timestamp", "end_timestamp", "model_name", "p", "page_size"} {
					parameters = append(parameters, map[string]any{"name": name, "in": "query", "required": false, "schema": properties[name]})
				}
				op["parameters"] = parameters
			} else {
				properties["user_ids"] = map[string]any{"type": "array", "maxItems": 16384, "items": idSchema, "description": "Required; duplicates are removed. The size limit applies before deduplication. Empty means no users."}
				op["parameters"] = []any{}
				op["requestBody"] = map[string]any{"required": true, "description": "At most 262144 bytes. Null fields are invalid.", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"type": "object", "properties": properties, "required": []string{"user_ids"}}}}}
			}
			response := op["responses"].(map[string]any)["200"].(map[string]any)
			page := map[string]any{"type": "object", "required": []string{"page", "page_size", "total", "items"}, "properties": map[string]any{
				"page": map[string]any{"type": "integer"}, "page_size": map[string]any{"type": "integer"}, "total": map[string]any{"type": "integer"},
				"items": map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/Log"}},
			}}
			envelope := wrapResponse(page)
			envelope["required"] = []string{"success", "message", "data"}
			response["content"].(map[string]any)["application/json"].(map[string]any)["schema"] = envelope
			declareErrorCodes(op, "400", "invalid_params")
			declareErrorCodes(op, "500", "internal_error")
		}
		if route.Path == "/api/log/aggregate" || route.Path == "/api/usage/aggregate" {
			op["requestBody"].(map[string]any)["required"] = true
			op["description"] = "Admin-only aggregate scoped to 1..100 explicit positive user IDs. IDs are deduplicated; unknown IDs reject the entire request. Inclusive nonnegative Unix-second range is filtered before SQL aggregation. Empty ID lists are rejected. Request body is limited to 16 KiB; response to 10000 rows and 4 MiB. No pagination, truncation or per-batch top-N. Usage grouping is time, model, time_model or user; granularity is required in all modes, but model/user fold the entire range. UTC weeks start Monday; month spans are limited to 120 months. Model identifiers are byte-exact. Counters and quota are int64 restricted to exact JSON integers. Log totals include CONSUME only."
			responses := op["responses"].(map[string]any)
			for status, code := range map[string]string{"400": "invalid_params", "404": "aggregate_user_not_found", "413": "aggregate_request_too_large", "422": "aggregate_response_too_large", "500": "aggregate_overflow"} {
				responses[status] = map[string]any{"description": code, "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/ApiResponse"}}}}
				declareErrorCodes(op, status, code)
			}
			declareErrorCodes(op, "500", "internal_error")
		}
		if h.RespStatus > 200 && h.RespStatus < 300 {
			responses := op["responses"].(map[string]interface{})
			responses[strconv.Itoa(h.RespStatus)] = responses["200"]
			delete(responses, "200")
		}
		if route.Method == "GET" && route.Path == "/api/system-task/current" && route.HandlerName == "GetCurrentSystemTask" {
			parameters, _ := op["parameters"].([]any)
			for _, parameter := range parameters {
				entry, _ := parameter.(map[string]any)
				if entry["name"] == "type" && entry["in"] == "query" {
					entry["schema"] = map[string]any{"type": "string"}
					entry["required"] = true
				}
			}
			data := structToSchema(modelTypes["SystemTaskResponse"])
			data["nullable"] = true
			response := op["responses"].(map[string]any)["200"].(map[string]any)
			response["content"].(map[string]any)["application/json"].(map[string]any)["schema"] = wrapResponse(data)
		}
		if route.Method == "GET" && route.Path == "/api/channel/test" && route.HandlerName == "TestAllChannels" {
			for _, status := range []string{"200", "409"} {
				properties := map[string]any{
					"task_id": map[string]any{"type": "string"},
					"status":  map[string]any{"type": "string"},
				}
				required := []string{"task_id", "status"}
				if status == "409" {
					properties["type"] = map[string]any{"type": "string"}
					required = append(required, "type")
				}
				schema := wrapResponse(map[string]any{"type": "object", "properties": properties, "required": required})
				schema["required"] = []string{"success", "message", "data"}
				schema["properties"].(map[string]any)["success"] = map[string]any{"type": "boolean", "enum": []bool{status == "200"}}
				response := op["responses"].(map[string]any)[status].(map[string]any)
				response["content"].(map[string]any)["application/json"].(map[string]any)["schema"] = schema
			}
		}
		if route.Method == "GET" && route.Path == "/api/channel/test/{id}" && route.HandlerName == "TestChannel" {
			for _, status := range []string{"422", "502"} {
				schema := map[string]any{
					"type":     "object",
					"required": []string{"success", "code", "message", "time", "data"},
					"properties": map[string]any{
						"success": map[string]any{"type": "boolean", "enum": []bool{false}},
						"code":    map[string]any{"type": "string", "enum": []string{"channel_test_failed"}},
						"message": map[string]any{"type": "string"},
						"time":    map[string]any{"type": "number"},
						"data":    map[string]any{"$ref": "#/components/schemas/ChannelTestResponse"},
					},
				}
				response := op["responses"].(map[string]any)[status].(map[string]any)
				response["content"].(map[string]any)["application/json"].(map[string]any)["schema"] = schema
			}
		}
		if route.HandlerName == "ProvisionGetAPIUser" {
			enrichGetAPIContract(op, true)
		}
		if route.HandlerName == "InitializeGetAPIPAT" {
			enrichGetAPIInitializeContract(op)
		}
		if route.HandlerName == "GetUserLogs" || route.HandlerName == "GetUserQuotaDates" {
			parameters, _ := op["parameters"].([]interface{})
			found := false
			for _, parameter := range parameters {
				entry, _ := parameter.(map[string]interface{})
				if entry["name"] != "token_id" || entry["in"] != "query" {
					continue
				}
				entry["schema"] = map[string]interface{}{"type": "integer", "minimum": 1}
				found = true
			}
			if !found {
				parameters = append(parameters, map[string]interface{}{"name": "token_id", "in": "query", "required": false, "description": "", "schema": map[string]interface{}{"type": "integer", "minimum": 1}})
			}
			op["parameters"] = parameters
		}
		if route.HandlerName == "GetAllQuotaDates" {
			declareGranularityParameter(op, "Without the username query parameter rows are grouped by model alone: model_name, created_at, count, quota, token_used carry data while user_id is 0 and username is empty in every row. With username set rows are grouped per user and model, and user_id and username are populated.")
			declareErrorCodes(op, "400", "invalid_params", "invalid_granularity", "month_span_exceeded")
		}
		if route.HandlerName == "GetUserQuotaDates" {
			declareGranularityParameter(op, "Rows are grouped per user and model and report user_id, username, model_name, created_at, count, quota, token_used.")
			declareErrorCodes(op, "400", "invalid_granularity")
		}
		if route.HandlerName == "GetQuotaDatesByUser" {
			declareGranularityParameter(op, "Rows are grouped per user and report username, created_at, count, quota, token_used.")
			declareErrorCodes(op, "400", "invalid_params", "invalid_granularity", "month_span_exceeded")
		}
		if route.HandlerName == "GetUsersBatch" {
			parameters, _ := op["parameters"].([]interface{})
			for _, parameter := range parameters {
				entry, _ := parameter.(map[string]interface{})
				if entry["name"] == "ids" && entry["in"] == "query" {
					entry["required"] = true
					entry["description"] = "Comma-separated list of 1 to 100 positive user IDs. Duplicate IDs are returned once in request order."
				}
			}
		}

		if route.HandlerName == "GetPublicPricingModels" {
			op["security"] = []any{}
			op["description"] = "Anonymous default-group customer tariffs. Required repeated tag parameters are normalized lowercase exact tokens and combined with AND. Filtering precedes deterministic model-name ordering and pagination. Unknown tags return an empty page. All other parameters, group overrides, duplicate singleton values, and malformed values are rejected. Prices include the default group markup; null means unknown and zero means free. Conditional alternatives retain structured guards."
			op["parameters"] = []any{
				map[string]any{"name": "catalog_type_missing", "in": "query", "required": false, "schema": map[string]any{"type": "boolean", "default": false}, "description": "True selects only models without a source catalog type; mutually exclusive with catalog_type. False or omitted preserves all types. Strict singular true/false; invalid or duplicate values return HTTP400."},
				map[string]any{"name": "catalog_type", "in": "query", "required": false, "schema": catalogTypeSchema(), "description": "Optional singular source taxonomy filter; AND with tags before pagination/counts. Invalid or duplicate value returns HTTP400."},
				map[string]any{"name": "tag", "in": "query", "required": true, "style": "form", "explode": true, "schema": map[string]any{"type": "array", "minItems": 1, "items": map[string]any{"type": "string", "minLength": 1}}, "description": "Repeated exact single tags; AND semantics. Commas, semicolons, pipes and internal whitespace are rejected."},
				map[string]any{"name": "p", "in": "query", "required": false, "schema": map[string]any{"type": "integer", "default": 1, "minimum": 1, "maximum": 9007199254740991}},
				map[string]any{"name": "page_size", "in": "query", "required": false, "schema": map[string]any{"type": "integer", "default": 10, "minimum": 1, "maximum": 100}},
			}
			responses := op["responses"].(map[string]any)
			responses["200"] = buildResponse(respSpec{Custom: "PublicPricingModelsResponse"})["200"]
			for _, code := range []string{"400", "503"} {
				responses[code] = map[string]any{"description": translate(currentLocale, "err."+code), "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/ApiErrorResponse"}}}}
			}
		}
		if route.HandlerName == "GetEffectivePricingModel" {
			op["description"] = "Customer prices and provider offers for one exact model visible to the authenticated user. Optional expected_revision rejects a changed catalog or tariff context."
			op["parameters"] = []any{
				map[string]any{"name": "model_name", "in": "query", "required": true, "schema": map[string]any{"type": "string", "minLength": 1}},
				map[string]any{"name": "expected_revision", "in": "query", "required": false, "schema": map[string]any{"type": "string", "minLength": 1, "maxLength": 256}},
			}
			responses := op["responses"].(map[string]any)
			for _, code := range []string{"400", "404", "409", "503"} {
				responses[code] = map[string]any{"description": translate(currentLocale, "err."+code), "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/ApiErrorResponse"}}}}
			}
		}
		if route.HandlerName == "GetEffectivePricingOverview" {
			op["description"] = "Bounded first pages for all six modalities from one immutable catalog and pricing context. Requires root dashboard access. Optional singular catalog_type filters before section counts/pagination; catalog_type_missing=true selects only absent types and conflicts with catalog_type, false/omitted preserves all types; invalid or duplicate values return HTTP400."
			parameters := []any{}
			for _, name := range []string{"limit", "search", "group", "using_group", "catalog_type", "catalog_type_missing"} {
				schema := map[string]any{"type": "string"}
				if name == "catalog_type" {
					schema = catalogTypeSchema()
				}
				if name == "catalog_type_missing" {
					schema = map[string]any{"type": "boolean", "default": false}
				}
				if name == "limit" {
					schema = map[string]any{"type": "integer", "default": 4, "minimum": 0, "maximum": 100}
				}
				parameters = append(parameters, map[string]any{"name": name, "in": "query", "required": false, "schema": schema})
			}
			op["parameters"] = parameters
			responses := op["responses"].(map[string]any)
			for _, code := range []string{"400", "503"} {
				responses[code] = map[string]any{"description": translate(currentLocale, "err."+code), "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/ApiErrorResponse"}}}}
			}
		}
		if route.HandlerName == "GetEffectivePricingByGroup" || route.HandlerName == "GetEffectivePricing" {
			parameters := []any{}
			for _, parameter := range []struct {
				name   string
				schema map[string]any
			}{
				{"limit", map[string]any{"type": "integer", "minimum": 0, "maximum": 100}},
				{"expected_revision", map[string]any{"type": "string", "minLength": 1, "maxLength": 256}},
				{"offset", map[string]any{"type": "integer", "default": 0, "minimum": 0, "maximum": 9007199254740991}},
				{"search", map[string]any{"type": "string"}},
				{"creator_id", map[string]any{"type": "integer"}},
				{"catalog_type", catalogTypeSchema()},
				{"catalog_type_missing", map[string]any{"type": "boolean", "default": false}},
				{"modality", map[string]any{"type": "string", "enum": []string{"TEXT", "IMAGE", "VIDEO", "EMBEDDINGS", "RERANK", "OTHER"}}},
			} {
				parameters = append(parameters, map[string]any{"name": parameter.name, "in": "query", "required": false, "schema": parameter.schema})
			}
			if route.HandlerName == "GetEffectivePricingByGroup" {
				for _, name := range []string{"group", "using_group"} {
					parameters = append(parameters, map[string]any{"name": name, "in": "query", "required": false, "schema": map[string]any{"type": "string"}})
				}
			} else {
				for _, parameter := range []struct {
					name   string
					values []string
				}{{"pricing_unit", []string{"TOKENS", "CALLS", "UNCONFIGURED"}}, {"order_by", []string{"NAME", "INPUT_PRICE", "OUTPUT_PRICE"}}, {"direction", []string{"asc", "desc"}}} {
					parameters = append(parameters, map[string]any{"name": parameter.name, "in": "query", "required": false, "schema": map[string]any{"type": "string", "enum": parameter.values}})
				}
			}
			op["parameters"] = parameters
			op["description"] = "Explicit limit selects native filtered pricing page mode with global creator facets; page metadata and row modality/creator are then required. No limit and no pagination parameters preserves the full legacy response. Pagination parameters without limit are rejected. Personal page mode rejects group overrides. Optional singular catalog_type is ANDed with modality before pagination/counts; catalog_type_missing=true selects only absent types and conflicts with catalog_type, false/omitted preserves legacy semantics; invalid or duplicate values return HTTP400."
			responses := op["responses"].(map[string]any)
			for _, code := range []string{"400", "503"} {
				responses[code] = map[string]any{"description": translate(currentLocale, "err."+code), "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/ApiErrorResponse"}}}}
			}
			responses["409"] = map[string]any{"description": "Catalog changed; restart traversal. Machine code: pricing_catalog_revision_mismatch.", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"type": "object", "properties": map[string]any{"success": map[string]any{"type": "boolean", "enum": []bool{false}}, "code": map[string]any{"type": "string", "enum": []string{"pricing_catalog_revision_mismatch"}}, "message": map[string]any{"type": "string"}}, "required": []string{"success", "code", "message"}}}}}
		}
		if route.HandlerName == "GetEffectivePricingByGroup" {
			op["description"] = op["description"].(string) + " Requires root dashboard access; group selects user context and using_group selects one usable price group in page mode."
			parameters, _ := op["parameters"].([]interface{})
			for _, parameter := range parameters {
				entry, _ := parameter.(map[string]interface{})
				if entry["name"] != "group" || entry["in"] != "query" {
					continue
				}
				entry["description"] = "Configured user group. Omitted or empty values use default."
				entry["schema"] = map[string]interface{}{"type": "string", "default": "default"}
			}
			responses := op["responses"].(map[string]interface{})
			responses["503"] = map[string]interface{}{
				"description": translate(currentLocale, "err.503"),
				"content": map[string]interface{}{
					"application/json": map[string]interface{}{
						"schema": map[string]interface{}{"$ref": "#/components/schemas/ApiErrorResponse"},
					},
				},
			}
		}
		if route.HandlerName == "VerifyLogin" || route.HandlerName == "LoginPasskeyFinish" {
			op["responses"].(map[string]interface{})["200"] = buildResponse(respSpec{Custom: "LoginSessionResponse"})["200"]
		}
		if route.Method == "GET" && route.Path == "/api/ratio_sync/channels" && route.HandlerName == "GetSyncableChannels" {
			referencedTypes["SyncableChannel"] = true
			schema := wrapResponse(map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/SyncableChannel"}})
			schema["required"] = []string{"success", "message", "data"}
			op["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"] = schema
		}
		if route.HandlerName == "FetchUpstreamRatios" {
			referencedTypes["DifferenceItem"] = true
			referencedTypes["TestResult"] = true
			op["responses"].(map[string]interface{})["200"] = buildResponse(respSpec{Custom: "RatioSyncResponse"})["200"]
		}
		if route.HandlerName == "UpdateChannel" {
			schema := structToSchema(modelTypes["Channel"])
			properties := schema["properties"].(map[string]any)
			delete(properties, "status")
			properties["key_mode"] = map[string]any{"type": "string", "nullable": true, "enum": []any{"append", "replace", nil}}
			properties["multi_key_mode"] = map[string]any{"type": "string", "nullable": true, "enum": []any{"random", "polling", nil}}
			delete(schema, "required")
			op["requestBody"] = map[string]any{
				"required": true,
				"content": map[string]any{
					"application/json": map[string]any{"schema": schema},
				},
			}
		}
		if route.HandlerName == "GetModelPricingConfig" {
			parameters, _ := op["parameters"].([]any)
			parameters = slices.DeleteFunc(parameters, func(parameter any) bool {
				entry, _ := parameter.(map[string]any)
				return entry["name"] == "model" && entry["in"] == "query"
			})
			op["parameters"] = append(parameters, map[string]any{
				"name":     "model",
				"in":       "query",
				"required": false,
				"style":    "form",
				"explode":  true,
				"schema": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "string"},
				},
			})
		}
		if route.HandlerName == "UpdateModelPricingConfig" {
			body := op["requestBody"].(map[string]interface{})
			body["required"] = true
			schema := body["content"].(map[string]interface{})["application/json"].(map[string]interface{})["schema"].(map[string]interface{})
			schema["required"] = []string{"changes"}
			schema["properties"].(map[string]interface{})["changes"].(map[string]interface{})["minItems"] = 1
		}
		if route.HandlerName == "SyncUpstreamModels" {
			body := op["requestBody"].(map[string]interface{})
			body["required"] = true
			content := body["content"].(map[string]interface{})["application/json"].(map[string]interface{})
			schema := content["schema"].(map[string]interface{})
			schema["required"] = []string{"source_version", "selections"}
			properties := schema["properties"].(map[string]interface{})
			properties["source_version"].(map[string]interface{})["minLength"] = 1
			properties["selections"].(map[string]interface{})["minItems"] = 1
		}
	}
}

func enrichGetAPIInitializeContract(op map[string]interface{}) {
	responses := map[string]interface{}{}
	for _, status := range []string{"200", "201", "400", "401", "403", "404", "409", "503"} {
		schema := "GetApiErrorResponse"
		if status == "200" || status == "201" {
			schema = "GetApiInitializePATResponse"
		}
		response := buildResponse(respSpec{Custom: schema})["200"].(map[string]interface{})
		response["headers"] = map[string]interface{}{"Cache-Control": map[string]interface{}{"schema": map[string]interface{}{"type": "string", "enum": []string{"no-store"}}}}
		responses[status] = response
	}
	op["responses"] = responses
	op["operationId"] = "initializeGetApiPat"
	op["parameters"] = []interface{}{map[string]interface{}{"name": "user_id", "in": "path", "required": true, "schema": map[string]interface{}{"type": "integer", "minimum": 1}}}
	op["requestBody"] = map[string]interface{}{"required": true, "content": map[string]interface{}{"application/json": map[string]interface{}{"schema": map[string]interface{}{"$ref": "#/components/schemas/GetAPIInitializePATRequest"}}}}
}

func enrichGetAPIInitializeSchemas(schemas map[string]interface{}) {
	if request, ok := schemas["GetAPIInitializePATRequest"].(map[string]interface{}); ok {
		request["additionalProperties"] = false
		request["required"] = []string{"expected_username"}
		props := request["properties"].(map[string]interface{})
		request["properties"] = map[string]interface{}{"expected_username": props["expected_username"], "apply": map[string]interface{}{"type": "boolean", "default": false}}
	}
	data := structToSchema(modelTypes["GetAPIInitializePATResult"])
	props := data["properties"].(map[string]interface{})
	props["state"] = map[string]interface{}{"type": "string", "enum": []string{"active", "blocked"}}
	props["outcome"] = map[string]interface{}{"type": "string", "enum": []string{"would_issue", "would_reuse", "issued", "reused", "blocked_no_pat"}}
	props["access_token"] = map[string]interface{}{"type": "string", "nullable": true}
	data["required"] = []string{"user_id", "state", "outcome", "access_token"}
	schemas["GetApiInitializePATResponse"] = wrapResponse(map[string]interface{}{"$ref": "#/components/schemas/GetAPIInitializePATResult"})
	schemas["GetApiInitializePATResponse"].(map[string]interface{})["required"] = []string{"success", "data"}
	schemas["GetAPIInitializePATResult"] = data
	if create, ok := schemas["GetApiCreateUserRequest"].(map[string]interface{}); ok {
		if props, ok := create["properties"].(map[string]interface{}); ok {
			delete(props, "external_account_id")
		}
		create["required"] = []string{"username", "password", "display_name"}
	}
	if cred, ok := schemas["GetApiCredential"].(map[string]interface{}); ok {
		if props, ok := cred["properties"].(map[string]interface{}); ok {
			delete(props, "external_account_id")
		}
		cred["required"] = []string{"user_id", "state", "access_token"}
	}
}

// Login delegates session issuance and challenge creation to non-handler
// functions, so its response cannot be inferred from Login's direct AST calls.
func enrichLoginSchemas(schemas map[string]interface{}) {
	userProperties := map[string]interface{}{}
	for _, name := range []string{"id", "role", "status", "quota", "used_quota", "request_count", "aff_count", "aff_quota", "aff_history_quota", "inviter_id"} {
		userProperties[name] = map[string]interface{}{"type": "integer"}
	}
	for _, name := range []string{"username", "display_name", "email", "github_id", "discord_id", "oidc_id", "wechat_id", "telegram_id", "group", "aff_code", "linux_do_id", "setting", "stripe_customer", "sidebar_modules"} {
		userProperties[name] = map[string]interface{}{"type": "string"}
	}
	userProperties["has_password"] = map[string]interface{}{"type": "boolean"}
	userProperties["permissions"] = map[string]interface{}{"type": "object", "additionalProperties": true}
	schemas["LoginUser"] = map[string]interface{}{"type": "object", "properties": userProperties, "required": []string{"id", "username", "role", "status"}}
	session := structToSchema(modelTypes["AuthBundle"])
	props := session["properties"].(map[string]interface{})
	props["user"] = map[string]interface{}{"$ref": "#/components/schemas/LoginUser"}
	session["required"] = []string{"access_token", "token_type", "access_expires_at", "session", "user"}
	schemas["LoginSessionData"] = session
	challenge := structToSchema(modelTypes["LoginChallenge"])
	challenge["required"] = []string{"require_verification", "flow_token", "expires_at", "methods"}
	challenge["properties"].(map[string]interface{})["require_verification"].(map[string]interface{})["enum"] = []bool{true}
	schemas["LoginChallenge"] = challenge
	schemas["LoginSessionView"] = structToSchema(modelTypes["LoginSessionView"])
	schemas["VerificationMethodOption"] = structToSchema(modelTypes["VerificationMethodOption"])
	schemas["LoginResponse"] = wrapResponse(map[string]interface{}{"oneOf": []interface{}{
		map[string]interface{}{"$ref": "#/components/schemas/LoginSessionData"},
		map[string]interface{}{"$ref": "#/components/schemas/LoginChallenge"},
	}})
	schemas["LoginSessionResponse"] = wrapResponse(map[string]interface{}{"$ref": "#/components/schemas/LoginSessionData"})
}

func enrichMetadataSelectionSchema(schemas map[string]interface{}) {
	selection := schemas["MetadataSyncSelection"].(map[string]interface{})
	selection["required"] = []string{"model_name", "record_version"}
	properties := selection["properties"].(map[string]interface{})
	properties["model_name"].(map[string]interface{})["minLength"] = 1
	properties["model_name"].(map[string]interface{})["pattern"] = `\S`
	properties["record_version"].(map[string]interface{})["minLength"] = 1
	fields := properties["fields"].(map[string]interface{})
	fields["items"].(map[string]interface{})["enum"] = []string{"description", "icon", "tags", "vendor", "endpoints", "name_rule", "status"}
	fields["nullable"] = true // creating a model does not require a field selection
	selection["oneOf"] = []interface{}{
		map[string]interface{}{"required": []string{"create"}, "properties": map[string]interface{}{"create": map[string]interface{}{"type": "boolean", "enum": []bool{true}}}},
		map[string]interface{}{"required": []string{"fields"}, "properties": map[string]interface{}{
			"create": map[string]interface{}{"type": "boolean", "enum": []bool{false}, "default": false},
			"fields": map[string]interface{}{"type": "array", "minItems": 1, "items": map[string]interface{}{"type": "string"}},
		}},
	}
}

func enrichGetAPIContract(op map[string]interface{}, provision bool) {
	statuses := []string{"400", "401", "403", "404", "409", "503"}
	if provision {
		statuses = append([]string{"201"}, statuses...)
	} else {
		statuses = append([]string{"200"}, statuses...)
	}
	responses := map[string]interface{}{}
	for _, status := range statuses {
		schema := "GetApiErrorResponse"
		if status == "200" || status == "201" {
			schema = "GetApiCreateCredentialResponse"
		}
		response := buildResponse(respSpec{Custom: schema})["200"].(map[string]interface{})
		response["description"] = "Request rejected; no credential material"
		if status == "200" || status == "201" {
			response["description"] = "Current credential snapshot"
		}
		response["headers"] = map[string]interface{}{"Cache-Control": map[string]interface{}{"schema": map[string]interface{}{"type": "string", "enum": []string{"no-store"}}}}
		responses[status] = response
	}
	op["responses"] = responses
	if provision {
		op["operationId"] = "provisionGetApiUser"
		op["parameters"] = []interface{}{}
		op["requestBody"] = map[string]interface{}{"required": true, "content": map[string]interface{}{"application/json": map[string]interface{}{"schema": map[string]interface{}{"$ref": "#/components/schemas/GetApiCreateUserRequest"}}}}
	}
}

func enrichPricingTransportSchemas(schemas map[string]interface{}) {
	for name, fields := range map[string][]string{
		"ModelPricingSnapshot": {"entries", "options", "empty_version"},
		"ModelPricingEntry":    {"model_name", "version", "configured", "effective"},
		"ModelPricingChange":   {"model_name", "expected_version"},
		"DifferenceItem":       {"current", "upstreams", "confidence"},
		"TestResult":           {"name", "status"},
		"SyncableChannel":      {"id", "name", "base_url", "status", "type"},
		"UpstreamDTO":          {"name", "base_url"},
	} {
		if schema, ok := schemas[name].(map[string]interface{}); ok {
			schema["required"] = fields
		}
	}
	if schema, ok := schemas["ModelPricingChange"].(map[string]interface{}); ok {
		schema["properties"].(map[string]interface{})["pricing"] = map[string]interface{}{
			"type": "object", "additionalProperties": exprToSchema(modelMaps["PricingValues"].Value), "nullable": true,
		}
	}
	if schema, ok := schemas["TestResult"].(map[string]any); ok {
		schema["properties"].(map[string]any)["status"] = map[string]any{"type": "string", "enum": []string{"success", "error"}}
	}
	scalar := map[string]any{"oneOf": []any{map[string]any{"type": "number", "nullable": true}, map[string]any{"type": "string"}}}
	if schema, ok := schemas["DifferenceItem"].(map[string]any); ok {
		properties := schema["properties"].(map[string]any)
		properties["current"] = scalar
		properties["upstreams"] = map[string]any{"type": "object", "additionalProperties": scalar}
	}
	pricingProperties := map[string]any{}
	for _, field := range []string{"model_ratio", "completion_ratio", "cache_ratio", "create_cache_ratio", "image_ratio", "audio_ratio", "audio_completion_ratio", "model_price"} {
		pricingProperties[field] = map[string]any{"type": "number", "minimum": 0}
	}
	pricingProperties["billing_mode"] = map[string]any{"type": "string", "enum": []string{"ratio", "tiered_expr"}}
	pricingProperties["billing_expr"] = map[string]any{"type": "string"}
	schemas["UpstreamPricingValues"] = map[string]any{"type": "object", "properties": pricingProperties, "additionalProperties": false}
	schemas["RatioSyncData"] = map[string]interface{}{
		"type":     "object",
		"required": []string{"differences", "prices", "test_results"},
		"properties": map[string]interface{}{
			"differences": map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{"$ref": "#/components/schemas/DifferenceItem"}}},
			"prices": map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{
				"type": "object", "required": []string{"current", "upstreams"},
				"properties": map[string]interface{}{
					"current":   map[string]interface{}{"$ref": "#/components/schemas/UpstreamPricingValues"},
					"upstreams": map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{"$ref": "#/components/schemas/UpstreamPricingValues"}},
				},
			}},
			"test_results": map[string]interface{}{"type": "array", "items": map[string]interface{}{"$ref": "#/components/schemas/TestResult"}},
		},
	}
	schemas["RatioSyncResponse"] = wrapResponse(map[string]interface{}{"$ref": "#/components/schemas/RatioSyncData"})
	schemas["RatioSyncResponse"].(map[string]interface{})["required"] = []string{"success", "data"}
}

func normalizeAggregateSchemas(schemas map[string]any) {
	for _, name := range []string{"AggregateScope", "UsageAggregateQuery"} {
		schema := schemas[name].(map[string]any)
		properties := schema["properties"].(map[string]any)
		properties["user_ids"] = map[string]any{"type": "array", "minItems": 1, "maxItems": 100, "items": map[string]any{"type": "integer", "format": "int64", "minimum": 1, "maximum": 9007199254740991}}
		for _, field := range []string{"start_timestamp", "end_timestamp"} {
			properties[field] = map[string]any{"type": "integer", "format": "int64", "minimum": 0, "maximum": 9007199254740991}
		}
		required := []string{"user_ids", "start_timestamp", "end_timestamp"}
		if name == "UsageAggregateQuery" {
			properties["group_by"] = map[string]any{"type": "string", "enum": []string{"time", "model", "time_model", "user"}}
			properties["granularity"] = map[string]any{"type": "string", "enum": []string{"hour", "day", "week", "month"}}
			required = append(required, "group_by", "granularity")
		}
		schema["required"] = required
	}
	for _, name := range []string{"LogAggregate", "UsageAggregateRow"} {
		schema := schemas[name].(map[string]any)
		if name == "LogAggregate" {
			schema["required"] = []string{"quota", "request_count"}
		} else {
			schema["required"] = []string{"quota", "count", "token_used"}
		}
		properties := schema["properties"].(map[string]any)
		for _, field := range []string{"quota", "count", "token_used", "request_count"} {
			if _, exists := properties[field]; exists {
				properties[field] = map[string]any{"type": "integer", "format": "int64", "minimum": -9007199254740991, "maximum": 9007199254740991}
			}
		}
	}
}
