package controller

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"golang.org/x/text/cases"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

type effectivePricingCreator struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type effectivePricingPageItem struct {
	consumerPricingModel
	Modality string                   `json:"modality"`
	Creator  *effectivePricingCreator `json:"creator"`
}

type effectivePricingPageData struct {
	consumerPricingResponse
	Offset     int                        `json:"offset"`
	Limit      int                        `json:"limit"`
	TotalCount int                        `json:"total_count"`
	Creators   []effectivePricingCreator  `json:"creators"`
	Data       []effectivePricingPageItem `json:"data"`
}

type effectivePricingPageQuery struct {
	limit            int
	offset           int
	search           string
	modality         string
	creatorID        *int
	pricingUnit      string
	orderBy          string
	direction        string
	expectedRevision string
}

type effectivePricingPageContext struct {
	metadata     consumerPricingResponse
	fxFactor     float64
	pricing      []model.Pricing
	vendors      []model.PricingVendor
	usableGroups map[string]string
	groupRatios  map[string]float64
	usingGroup   string
}

var pricingPageInteger = regexp.MustCompile(`^-?[0-9]+$`)
var pricingSpeechFamily = regexp.MustCompile(`(^tts-)|(-tts(-|$))`)
var pricingTranscriptionFamily = regexp.MustCompile(`(^whisper-)|(-transcribe(-|$))`)

func parseEffectivePricingPageQuery(values url.Values, personal bool) (effectivePricingPageQuery, error) {
	query := effectivePricingPageQuery{limit: 20, orderBy: "NAME", direction: "asc"}
	revision, err := expectedPricingRevision(values)
	if err != nil {
		return query, err
	}
	query.expectedRevision = revision
	for key, entries := range values {
		if len(entries) != 1 {
			return query, fmt.Errorf("invalid %s", key)
		}
		if personal && (key == "group" || key == "using_group") {
			return query, fmt.Errorf("group overrides are not allowed")
		}
	}
	for _, parameter := range []struct {
		name    string
		target  *int
		maximum int64
	}{{"limit", &query.limit, 100}, {"offset", &query.offset, 9007199254740991}} {
		if raw, present := values[parameter.name]; present {
			if !pricingPageInteger.MatchString(raw[0]) {
				return query, fmt.Errorf("invalid %s", parameter.name)
			}
			number, err := strconv.ParseInt(raw[0], 10, 64)
			if err != nil || number < 0 || number > parameter.maximum {
				return query, fmt.Errorf("invalid %s", parameter.name)
			}
			*parameter.target = int(number)
		}
	}
	if raw, present := values["creator_id"]; present {
		if !pricingPageInteger.MatchString(raw[0]) {
			return query, fmt.Errorf("invalid creator_id")
		}
		number, err := strconv.ParseInt(raw[0], 10, 32)
		if err != nil {
			return query, fmt.Errorf("invalid creator_id")
		}
		id := int(number)
		query.creatorID = &id
	}
	query.search = cases.Lower(language.Und).String(trimPricingText(values.Get("search")))
	for _, parameter := range []struct {
		name    string
		target  *string
		allowed []string
	}{
		{"modality", &query.modality, []string{"TEXT", "IMAGE", "VIDEO", "EMBEDDINGS", "RERANK", "OTHER"}},
		{"pricing_unit", &query.pricingUnit, []string{"TOKENS", "CALLS", "UNCONFIGURED"}},
		{"order_by", &query.orderBy, []string{"NAME", "INPUT_PRICE", "OUTPUT_PRICE"}},
		{"direction", &query.direction, []string{"asc", "desc"}},
	} {
		if raw, present := values[parameter.name]; present {
			if (!personal && parameter.name != "modality") || !slices.Contains(parameter.allowed, raw[0]) {
				return query, fmt.Errorf("invalid %s", parameter.name)
			}
			*parameter.target = raw[0]
		}
	}
	return query, nil
}

func routeEffectivePricingPage(c *gin.Context, personal bool) bool {
	values, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid pricing query"})
		return true
	}
	if _, present := values["limit"]; present {
		if personal {
			getEffectivePricingPage(c, values)
		} else {
			getEffectivePricingPageByGroup(c, values)
		}
		return true
	}
	for _, name := range []string{"offset", "search", "modality", "creator_id", "using_group", "pricing_unit", "order_by", "direction"} {
		if _, present := values[name]; present {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "limit is required for pricing pagination"})
			return true
		}
	}
	return false
}

func getEffectivePricingPage(c *gin.Context, values url.Values) {
	query, err := parseEffectivePricingPageQuery(values, true)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	user, err := model.GetUserCache(c.GetInt("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to resolve user pricing group"})
		return
	}
	serveEffectivePricingPage(c, user.Group, "", query)
}

func getEffectivePricingPageByGroup(c *gin.Context, values url.Values) {
	query, err := parseEffectivePricingPageQuery(values, false)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	group := strings.TrimSpace(values.Get("group"))
	if group == "" {
		group = "default"
	}
	if group == "auto" || !ratio_setting.ContainsGroupRatio(group) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid user pricing group"})
		return
	}
	usingGroup := strings.TrimSpace(values.Get("using_group"))
	if usingGroup == "" {
		usingGroup = group
	}
	serveEffectivePricingPage(c, group, usingGroup, query)
}

func serveEffectivePricingPage(c *gin.Context, userGroup, usingGroup string, query effectivePricingPageQuery) {
	context, err := loadConsumerPricingContext(userGroup, usingGroup)
	if err != nil {
		status := http.StatusServiceUnavailable
		if err == errPricingUsingGroup {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"success": false, "message": err.Error()})
		return
	}
	if !pricingRevisionMatches(c, query.expectedRevision, context.metadata.CatalogRevision) {
		return
	}
	response := buildEffectivePricingPage(context, query, context.project)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": response})
}

func (context effectivePricingPageContext) project(item model.Pricing) consumerPricingModel {
	return buildConsumerPricingModel(context.usableGroups, context.groupRatios, context.fxFactor, item)
}

type effectivePricingPageCandidate struct {
	index    int
	modality string
	creator  *effectivePricingCreator
	price    *float64
}

func buildEffectivePricingPage(context effectivePricingPageContext, query effectivePricingPageQuery, project func(model.Pricing) consumerPricingModel) effectivePricingPageData {
	context.pricing = model.ConsumerPricingCatalogue(context.pricing, context.usableGroups)
	response := effectivePricingPageData{consumerPricingResponse: context.metadata, Offset: query.offset, Limit: query.limit, Creators: []effectivePricingCreator{}, Data: []effectivePricingPageItem{}}
	creators := make(map[int]*effectivePricingCreator, len(context.vendors))
	for _, vendor := range context.vendors {
		if name := trimPricingText(vendor.Name); name != "" {
			creators[vendor.ID] = &effectivePricingCreator{ID: vendor.ID, Name: name}
		}
	}
	usedCreators := make(map[int]bool)
	candidates := make([]effectivePricingPageCandidate, 0, len(context.pricing))
	lower := cases.Lower(language.Und)
	for index, item := range context.pricing {
		if len(effectivePricingGroups(item.EnableGroup, context.usableGroups)) == 0 {
			continue
		}
		if context.usingGroup != "" && len(item.BillingSourcesForGroup(context.usingGroup)) == 0 {
			if _, err := service.ApplyBillingFX(context.groupRatios[context.usingGroup], context.fxFactor); err != nil {
				continue
			}
		}
		creator := creators[item.VendorID]
		if item.VendorID == 0 {
			creator = nil
		}
		if creator != nil && !usedCreators[creator.ID] {
			response.Creators = append(response.Creators, *creator)
			usedCreators[creator.ID] = true
		}
		modality := effectivePricingModality(item)
		if query.search != "" && !strings.Contains(lower.String(item.ModelName), query.search) {
			continue
		}
		if query.modality != "" && modality != query.modality {
			continue
		}
		if query.creatorID != nil && (creator == nil || creator.ID != *query.creatorID) {
			continue
		}
		candidate := effectivePricingPageCandidate{index: index, creator: creator, modality: modality}
		if query.pricingUnit != "" || query.orderBy != "NAME" {
			projected := project(item)
			unit, price := effectivePricingPageKeys(projected, query.orderBy)
			if query.pricingUnit != "" && unit != query.pricingUnit {
				continue
			}
			candidate.price = price
		}
		candidates = append(candidates, candidate)
	}
	collator := collate.New(language.English)
	slices.SortFunc(response.Creators, func(left, right effectivePricingCreator) int {
		if order := collator.CompareString(left.Name, right.Name); order != 0 {
			return order
		}
		return left.ID - right.ID
	})
	slices.SortStableFunc(candidates, func(left, right effectivePricingPageCandidate) int {
		if query.orderBy != "NAME" {
			if left.price == nil && right.price != nil {
				return 1
			}
			if left.price != nil && right.price == nil {
				return -1
			}
			if left.price != nil && right.price != nil && *left.price != *right.price {
				order := -1
				if *left.price > *right.price {
					order = 1
				}
				if query.direction == "desc" {
					order = -order
				}
				return order
			}
		}
		order := compareEffectivePricingNames(context.pricing[left.index].ModelName, context.pricing[right.index].ModelName)
		if query.orderBy == "NAME" && query.direction == "desc" {
			return -order
		}
		return order
	})
	response.TotalCount = len(candidates)
	start := min(query.offset, len(candidates))
	end := start + min(query.limit, len(candidates)-start)
	for _, candidate := range candidates[start:end] {
		response.Data = append(response.Data, effectivePricingPageItem{consumerPricingModel: project(context.pricing[candidate.index]), Modality: candidate.modality, Creator: candidate.creator})
	}
	return response
}

func compareEffectivePricingNames(left, right string) int {
	lower := cases.Lower(language.Und)
	if order := slices.Compare(utf16.Encode([]rune(lower.String(left))), utf16.Encode([]rune(lower.String(right)))); order != 0 {
		return order
	}
	return slices.Compare(utf16.Encode([]rune(left)), utf16.Encode([]rune(right)))
}

func effectivePricingModality(item model.Pricing) string {
	proved := make(map[string]bool)
	for _, offers := range item.CatalogOffersByGroup {
		for _, offer := range offers {
			switch offer.Operation {
			case "image_generation", "image_editing":
				proved["IMAGE"] = true
			case "text_to_video", "image_to_video", "video_to_video", "video_editing", "video_upscaling", "remix":
				proved["VIDEO"] = true
			case "embeddings":
				proved["EMBEDDINGS"] = true
			case "rerank":
				proved["RERANK"] = true
			case "speech_generation", "transcription", "realtime":
				proved["OTHER"] = true
			case "text":
				proved["TEXT"] = true
			}
		}
	}
	for _, modality := range []string{"IMAGE", "VIDEO", "EMBEDDINGS", "RERANK", "OTHER", "TEXT"} {
		if proved[modality] {
			return modality
		}
	}
	endpoints := make([]string, 0, len(item.SupportedEndpointTypes))
	name := strings.ToLower(item.ModelName[strings.LastIndex(item.ModelName, "/")+1:])
	for _, endpoint := range item.SupportedEndpointTypes {
		normalized := strings.ToLower(string(endpoint))
		if normalized == "openai" {
			switch {
			case strings.HasPrefix(name, "sora-"):
				normalized = "openai-video"
			case strings.HasPrefix(name, "dall-e-"), strings.HasPrefix(name, "gpt-image-"), strings.HasPrefix(name, "chatgpt-image-"):
				normalized = "image-generation"
			case strings.HasPrefix(name, "text-embedding-"):
				normalized = "embeddings"
			case pricingSpeechFamily.MatchString(name):
				normalized = "audio-speech"
			case pricingTranscriptionFamily.MatchString(name):
				normalized = "audio-transcription"
			}
		}
		endpoints = append(endpoints, normalized)
	}
	for _, category := range []struct{ fragment, modality string }{{"image", "IMAGE"}, {"video", "VIDEO"}, {"embedding", "EMBEDDINGS"}, {"rerank", "RERANK"}, {"audio", "OTHER"}} {
		for _, endpoint := range endpoints {
			if strings.Contains(endpoint, category.fragment) {
				return category.modality
			}
		}
	}
	return "TEXT"
}

func effectivePricingPageKeys(item consumerPricingModel, orderBy string) (string, *float64) {
	unit := "UNCONFIGURED"
	var minimum *float64
	component := "input"
	if orderBy == "OUTPUT_PRICE" {
		component = "output"
	}
	for _, group := range item.GroupPrices {
		for _, operation := range group.Operations {
			if operation.Mode == "batch" {
				continue
			}
			for _, price := range operation.Prices {
				if price.AmountRub == nil {
					continue
				}
				if unit == "UNCONFIGURED" {
					unit = "NATIVE"
				}
				if price.Unit == "call" {
					unit = "CALLS"
				} else if price.Unit == "million_tokens" && unit != "CALLS" {
					unit = "TOKENS"
				}
				if price.Component == component && price.Unit == "million_tokens" && (minimum == nil || *price.AmountRub < *minimum) {
					amount := *price.AmountRub
					minimum = &amount
				}
			}
		}
	}
	return unit, minimum
}

func trimPricingText(value string) string {
	return strings.TrimFunc(value, func(character rune) bool {
		return character == '\ufeff' || (character != '\u0085' && unicode.IsSpace(character))
	})
}
