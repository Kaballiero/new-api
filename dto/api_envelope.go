package dto

// ApiErrorResponse is the body shape returned by all admin/dashboard API
// handlers when an error occurs (HTTP 4xx or 5xx). It extends the standard
// success/message envelope with a stable, machine-readable `code` field that
// clients can switch on without parsing the (i18n-translated) message.
//
// Producers: helpers in common/gin.go — ApiErrorStatusCode,
// ApiErrorMsgStatusCode, ApiErrorI18nStatusCode.
//
// Consumers: the SDK in `getapi-backend` reads `code` to map server-side
// failures to typed exceptions; the web frontend reads `message` for toast.
//
// Note: legacy handlers may still emit `{success:false, message:...}` without
// `code` and with HTTP 200 — those paths are being migrated wave-by-wave. The
// schema here describes the *target* shape; older endpoints conform partially.
type ApiErrorResponse struct {
	Success bool   `json:"success"`           // always false for error responses
	Code    string `json:"code,omitempty"`    // stable snake_case error id (e.g. "user_not_found")
	Message string `json:"message"`           // i18n-translated human-readable message
}
