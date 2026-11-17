package openrouter

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/QuantumNous/new-api/common"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
)

// webSearchEngine is the engine of an OpenRouter web search: the engine of the
// web plugin and of the openrouter:web_search server tool.
type webSearchEngine string

const (
	webSearchEngineAuto   webSearchEngine = "auto"
	webSearchEngineNative webSearchEngine = "native"
)

// nativeWebSearchEngines search with the provider of the endpoint, at its
// web_search price; an absent engine is auto, which takes native search where
// the provider publishes it. Other engines (exa, parallel, firecrawl,
// perplexity) have no endpoint price.
var nativeWebSearchEngines = []webSearchEngine{"", webSearchEngineAuto, webSearchEngineNative}

// maxToolCalls is the server tool budget of an OpenRouter request:
// max_tool_calls defaults to it, which is also its maximum.
const maxToolCalls = 30

var errUnpricedExecutionDimensions = errors.New("OpenRouter router, preset or plugins have unpriced execution dimensions")

// requestedSearches counts the web searches an OpenRouter request may run: one
// for the web plugin and one for web_search_options, max_uses or maxToolCalls
// for the openrouter:web_search server tool, all together at most
// max_tool_calls and maxToolCalls. native reports that every search runs on a
// native engine (nativeWebSearchEngines). Search is admitted for text output
// only; other plugins and hosted tools stay refused.
func requestedSearches(body []byte, textOutput bool) (searches int, native bool, err error) {
	var request struct {
		Plugins []json.RawMessage `json:"plugins"`
		Tools   []struct {
			Type       string          `json:"type"`
			Parameters json.RawMessage `json:"parameters"`
		} `json:"tools"`
		WebSearchOptions json.RawMessage `json:"web_search_options"`
		MaxToolCalls     json.RawMessage `json:"max_tool_calls"`
	}
	if common.Unmarshal(body, &request) != nil {
		return 0, false, errors.New("invalid OpenRouter final request")
	}
	native = true
	for _, raw := range request.Plugins {
		var plugin struct {
			ID     string          `json:"id"`
			Engine webSearchEngine `json:"engine"`
		}
		if common.Unmarshal(raw, &plugin) != nil || plugin.ID != "web" || !textOutput {
			return 0, false, errUnpricedExecutionDimensions
		}
		searches = min(searches+1, maxToolCalls)
		native = native && slices.Contains(nativeWebSearchEngines, plugin.Engine)
	}
	for _, tool := range request.Tools {
		if tool.Type == "function" || tool.Type == "custom" {
			continue
		}
		if tool.Type != "openrouter:web_search" || !textOutput {
			return 0, false, errors.New("OpenRouter hosted tool pricing is unavailable")
		}
		var parameters struct {
			Engine  webSearchEngine `json:"engine"`
			MaxUses json.RawMessage `json:"max_uses"`
		}
		if presentJSON(tool.Parameters) && common.Unmarshal(tool.Parameters, &parameters) != nil {
			return 0, false, errors.New("invalid OpenRouter web search parameters")
		}
		uses := maxToolCalls
		if presentJSON(parameters.MaxUses) {
			if uses, err = toolCallLimit(parameters.MaxUses, "max_uses"); err != nil {
				return 0, false, err
			}
		}
		searches = min(searches+min(uses, maxToolCalls), maxToolCalls)
		native = native && slices.Contains(nativeWebSearchEngines, parameters.Engine)
	}
	if presentJSON(request.WebSearchOptions) {
		if !textOutput {
			return 0, false, pricingdomain.ErrHostedSearchUnpriced
		}
		searches = min(searches+1, maxToolCalls)
	}
	if searches > 0 && presentJSON(request.MaxToolCalls) {
		limit, err := toolCallLimit(request.MaxToolCalls, "max_tool_calls")
		if err != nil {
			return 0, false, err
		}
		searches = min(searches, limit)
	}
	return searches, native, nil
}

// toolCallLimit reads max_uses or max_tool_calls: a positive integer.
func toolCallLimit(raw json.RawMessage, field string) (int, error) {
	var limit int
	if common.Unmarshal(raw, &limit) != nil || limit < 1 {
		return 0, fmt.Errorf("invalid OpenRouter %s", field)
	}
	return limit, nil
}
