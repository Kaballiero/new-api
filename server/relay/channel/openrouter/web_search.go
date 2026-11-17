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

// requestedSearches counts the web searches an OpenRouter request may run on
// a native engine (nativeWebSearchEngines): the openrouter:web_search server
// tools max_uses each, or maxToolCalls, together at most max_tool_calls and
// maxToolCalls, plus one for the web plugin and one for web_search_options.
// manual reports a search on another engine, which has no endpoint price.
// Search is admitted for text output only; other plugins and hosted tools stay
// refused, and so does stop_server_tools_when, whose stop rules the count
// cannot bound.
func requestedSearches(body []byte, textOutput bool) (searches int, manual bool, err error) {
	var request struct {
		Plugins []json.RawMessage `json:"plugins"`
		Tools   []struct {
			Type       string          `json:"type"`
			Parameters json.RawMessage `json:"parameters"`
		} `json:"tools"`
		WebSearchOptions    json.RawMessage `json:"web_search_options"`
		MaxToolCalls        json.RawMessage `json:"max_tool_calls"`
		StopServerToolsWhen json.RawMessage `json:"stop_server_tools_when"`
	}
	if common.Unmarshal(body, &request) != nil {
		return 0, false, errors.New("invalid OpenRouter final request")
	}
	webPlugin := false
	for _, raw := range request.Plugins {
		var plugin struct {
			ID     string          `json:"id"`
			Engine webSearchEngine `json:"engine"`
		}
		if common.Unmarshal(raw, &plugin) != nil || plugin.ID != "web" || !textOutput {
			return 0, false, errUnpricedExecutionDimensions
		}
		if slices.Contains(nativeWebSearchEngines, plugin.Engine) {
			webPlugin = true
		} else {
			manual = true
		}
	}
	serverTool := false
	for _, tool := range request.Tools {
		if tool.Type == "function" || tool.Type == "custom" {
			continue
		}
		if tool.Type != "openrouter:web_search" || !textOutput {
			return 0, false, errors.New("OpenRouter hosted tool pricing is unavailable")
		}
		serverTool = true
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
		if !slices.Contains(nativeWebSearchEngines, parameters.Engine) {
			manual = true
			continue
		}
		searches = min(searches+min(uses, maxToolCalls), maxToolCalls)
	}
	if serverTool && presentJSON(request.MaxToolCalls) {
		limit, err := toolCallLimit(request.MaxToolCalls, "max_tool_calls")
		if err != nil {
			return 0, false, err
		}
		searches = min(searches, limit)
	}
	if webPlugin {
		searches++
	}
	if presentJSON(request.WebSearchOptions) {
		if !textOutput {
			return 0, false, pricingdomain.ErrHostedSearchUnpriced
		}
		searches++
	}
	if (searches > 0 || manual) && presentJSON(request.StopServerToolsWhen) {
		return 0, false, errUnpricedExecutionDimensions
	}
	return searches, manual, nil
}

// toolCallLimit reads max_uses or max_tool_calls: a positive integer.
func toolCallLimit(raw json.RawMessage, field string) (int, error) {
	var limit int
	if common.Unmarshal(raw, &limit) != nil || limit < 1 {
		return 0, fmt.Errorf("invalid OpenRouter %s", field)
	}
	return limit, nil
}
