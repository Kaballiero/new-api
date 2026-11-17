package openrouter

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	tokenusage "github.com/QuantumNous/new-api/gateway/tokenusage"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// Cohere search units (cohere.com/pricing): a search unit is one query with up
// to cohereSearchUnitDocuments documents; a document longer than
// cohereChunkTokens tokens, the query included, is split into chunks, and each
// chunk counts as a document.
const (
	cohereSearchUnitDocuments = 100
	cohereChunkTokens         = 500
)

// rerankUnits measures a rerank request in the units a rerank endpoint prices:
// the Cohere search units, and the input tokens of a token-priced provider by
// the Voyage rule, the query tokens times the documents plus the document
// tokens. A document object counts as its JSON text. Truncation by
// max_tokens_per_doc is not credited, so both are upper bounds. The request
// body is at most the pricing inspection limit, so every count fits int64.
func rerankUnits(body []byte, model string) (map[pricingdomain.PriceDimension]int64, error) {
	var request dto.RerankRequest
	if common.Unmarshal(body, &request) != nil {
		return nil, errors.New("invalid OpenRouter rerank request")
	}
	queryTokens := int64(tokenusage.CountTextToken(request.Query, model))
	chunkTokens := max(1, cohereChunkTokens-queryTokens)
	var chunks, documentTokens int64
	for _, document := range request.Documents {
		text, isText := document.(string)
		if !isText {
			// A decoded JSON value always encodes again.
			encoded, _ := common.Marshal(document)
			text = string(encoded)
		}
		tokens := int64(tokenusage.CountTextToken(text, model))
		chunks += max(1, (tokens+chunkTokens-1)/chunkTokens)
		documentTokens += tokens
	}
	searches := max(1, (chunks+cohereSearchUnitDocuments-1)/cohereSearchUnitDocuments)
	return map[pricingdomain.PriceDimension]int64{
		{Billable: pricingdomain.PriceBillableSearch, Unit: pricingdomain.PriceUnitSearch}:           searches,
		{Billable: pricingdomain.PriceBillableInputText, Unit: pricingdomain.PriceUnitMillionTokens}: queryTokens*int64(len(request.Documents)) + documentTokens,
	}, nil
}
