package service

import (
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/types"
)

// ModelLiveness is the tri-state result of validating a single model against an
// upstream channel via a real test request.
//
//   - ModelAlive     — the test request succeeded.
//   - ModelDead      — the upstream definitively reports the model does not exist
//     / is no longer available / is not a valid id. Safe to drop.
//   - ModelUncertain — the failure is NOT proof the model is gone: wrong test
//     endpoint (responses/images/audio-only models), rate limit, payment/auth,
//     not-implemented, transport/timeout. Keep the model, surface for review.
type ModelLiveness string

const (
	ModelAlive     ModelLiveness = "alive"
	ModelDead      ModelLiveness = "dead"
	ModelUncertain ModelLiveness = "uncertain"
)

// modelWrongEndpointKeywords mark a failure caused by hitting the wrong endpoint
// for a real model (e.g. gpt-5-pro is /v1/responses-only, dall-e needs images,
// *-tts needs audio/speech). These must classify as uncertain — the catalog
// entry is genuine, we simply can't prove liveness over chat/completions.
var modelWrongEndpointKeywords = []string{
	"only supported in v1/responses",
	"not a chat model",
	"not a chat completions",
	"unknown parameter",
	"use the responses api",
	"v1/images",
	"v1/audio",
	"v1/responses",
	"/responses",
}

// modelDeadKeywords are definitive "this model id does not exist / no access"
// signals from the upstream provider. Only these (plus a bare 404) drop a model.
var modelDeadKeywords = []string{
	"does not exist",
	"no longer available",
	"not a valid model id",
	"model not found",
	"invalid model",
	"unknown model",
}

// upstreamStatusRe extracts the real upstream status code from the error string
// built by RelayErrorHandler ("bad response status code %d, ...").
//
// testChannel overwrites NewAPIError.StatusCode with 500 on any non-200
// upstream response (controller/channel-test.go), so the original status survives
// only inside the error text — parse it from there, never trust StatusCode.
var upstreamStatusRe = regexp.MustCompile(`bad response status code (\d{3})`)

// parseUpstreamStatusCode returns the upstream HTTP status embedded in a
// (lower-cased) error message, or 0 when none is present.
func parseUpstreamStatusCode(lower string) int {
	m := upstreamStatusRe.FindStringSubmatch(lower)
	if len(m) < 2 {
		return 0
	}
	code := 0
	for _, r := range m[1] {
		code = code*10 + int(r-'0')
	}
	return code
}

// ClassifyModelValidation maps a testChannel result to a tri-state liveness plus
// the parsed upstream status code (0 when unknown). It is intentionally
// conservative: a model is only ModelDead on definitive not-exist evidence;
// every other failure is ModelUncertain so a real model is never silently dropped.
//
// It deliberately does NOT reuse ShouldDisableChannel: that is gated by
// AutomaticDisableChannelEnabled, treats 401/403 as ban-worthy (the opposite of
// what model liveness needs), and is boolean rather than tri-state. Only the
// lower-level AcSearch primitive is shared.
func ClassifyModelValidation(localErr error, apiErr *types.NewAPIError) (ModelLiveness, int) {
	if localErr == nil && apiErr == nil {
		return ModelAlive, 200
	}

	var msg string
	if apiErr != nil {
		msg = apiErr.Error()
	} else {
		msg = localErr.Error()
	}
	lower := strings.ToLower(msg)
	code := parseUpstreamStatusCode(lower)

	// Wrong-endpoint shapes first: a real model tested over the wrong path.
	if hit, _ := AcSearch(lower, modelWrongEndpointKeywords, true); hit {
		return ModelUncertain, code
	}

	// Definitive not-exist signals.
	if hit, _ := AcSearch(lower, modelDeadKeywords, true); hit {
		return ModelDead, code
	}

	// Status-code heuristics for messages without a recognizable phrase.
	switch code {
	case 404:
		// 404 with no wrong-endpoint hint ⇒ the model id is gone.
		return ModelDead, code
	case 400:
		// 400 not matched by a dead keyword ⇒ request shape / parameter issue.
		return ModelUncertain, code
	case 401, 403, 429, 500, 502, 503:
		// auth / payment / rate-limit / not-implemented ⇒ transient, keep.
		return ModelUncertain, code
	}

	// Transport errors, timeouts, local failures ⇒ never dead.
	return ModelUncertain, code
}

// EndpointFromErrorText infers the endpoint a model should be tested on from a
// wrong-endpoint upstream error (e.g. "this model is only supported in
// v1/responses"). It returns the target endpoint type and true when the error
// points at a retryable endpoint. Audio (v1/audio) is intentionally not returned
// here — there is no audio test endpoint type yet, so such failures stay uncertain.
func EndpointFromErrorText(msg string) (constant.EndpointType, bool) {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "only supported in v1/responses"),
		strings.Contains(lower, "use the responses api"),
		strings.Contains(lower, "not a chat model"),
		strings.Contains(lower, "not a chat completions"),
		strings.Contains(lower, "v1/responses"):
		return constant.EndpointTypeOpenAIResponse, true
	case strings.Contains(lower, "v1/images"):
		return constant.EndpointTypeImageGeneration, true
	case strings.Contains(lower, "v1/embeddings"):
		return constant.EndpointTypeEmbeddings, true
	case strings.Contains(lower, "v1/rerank"):
		return constant.EndpointTypeJinaRerank, true
	}
	return "", false
}
