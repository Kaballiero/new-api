// Package transport holds the incoming HTTP code shared by pricing scenarios:
// query parsing and the mapping of domain errors to HTTP responses.
package transport

import (
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/internal/pricing/domain"
)

// ParsePublicPricingModelsQuery parses the query of the public model catalog.
func ParsePublicPricingModelsQuery(raw string) (domain.PublicPricingModelsQuery, error) {
	query := domain.PublicPricingModelsQuery{Page: 1, PageSize: 10}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return query, fmt.Errorf("invalid pricing query")
	}
	revision, err := ExpectedPricingRevision(values)
	if err != nil {
		return query, err
	}
	query.ExpectedRevision = revision
	query.CatalogTypeMissing, err = ParseCatalogTypeMissing(values)
	if err != nil {
		return query, err
	}
	for key, entries := range values {
		if key != "tag" && key != "p" && key != "page_size" && key != "expected_revision" && key != "catalog_type" && key != "catalog_type_missing" {
			return query, fmt.Errorf("unsupported %s", key)
		}
		if key != "tag" && len(entries) != 1 {
			return query, fmt.Errorf("invalid %s", key)
		}
	}
	if raw, present := values["catalog_type"]; present {
		if !slices.Contains(domain.CatalogModelTypes(), raw[0]) {
			return query, fmt.Errorf("invalid catalog_type")
		}
		query.CatalogType = raw[0]
	}
	if len(values["tag"]) == 0 {
		return query, fmt.Errorf("tag is required")
	}
	for _, value := range values["tag"] {
		normalized := strings.ToLower(strings.TrimSpace(value))
		tags := domain.PricingTags(normalized)
		if len(tags) != 1 || tags[0] != normalized {
			return query, fmt.Errorf("invalid tag")
		}
		query.Tags = append(query.Tags, normalized)
	}
	slices.Sort(query.Tags)
	query.Tags = slices.Compact(query.Tags)
	for _, param := range []struct {
		name    string
		target  *int
		maximum int64
	}{
		{"p", &query.Page, 9007199254740991}, {"page_size", &query.PageSize, 100},
	} {
		if entries, ok := values[param.name]; ok {
			if entries[0] == "" || strings.IndexFunc(entries[0], func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
				return query, fmt.Errorf("invalid %s", param.name)
			}
			number, err := strconv.ParseInt(entries[0], 10, 64)
			if err != nil || number < 1 || number > param.maximum {
				return query, fmt.Errorf("invalid %s", param.name)
			}
			*param.target = int(number)
		}
	}
	return query, nil
}

// ParseCatalogTypeMissing keeps false equivalent to an omitted selector.
func ParseCatalogTypeMissing(values url.Values) (bool, error) {
	raw, present := values["catalog_type_missing"]
	if !present {
		return false, nil
	}
	if len(raw) != 1 || raw[0] != "true" && raw[0] != "false" {
		return false, fmt.Errorf("invalid catalog_type_missing")
	}
	missing := raw[0] == "true"
	if missing && values.Get("catalog_type") != "" {
		return false, fmt.Errorf("catalog_type_missing conflicts with catalog_type")
	}
	return missing, nil
}

// ExpectedPricingRevision reads the optional expected_revision selector.
func ExpectedPricingRevision(values url.Values) (string, error) {
	entries, exists := values["expected_revision"]
	if !exists {
		return "", nil
	}
	if len(entries) != 1 || entries[0] == "" || len(entries[0]) > 256 {
		return "", fmt.Errorf("invalid expected_revision")
	}
	return entries[0], nil
}

// ParseEffectivePricingPageQuery parses a page of the effective pricing catalog.
func ParseEffectivePricingPageQuery(values url.Values, personal bool) (domain.EffectivePricingPageQuery, error) {
	query := domain.EffectivePricingPageQuery{Personal: personal, Limit: 20, OrderBy: "NAME", Direction: "asc"}
	revision, err := ExpectedPricingRevision(values)
	if err != nil {
		return query, err
	}
	query.ExpectedRevision = revision
	query.CatalogTypeMissing, err = ParseCatalogTypeMissing(values)
	if err != nil {
		return query, err
	}
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
	}{{"limit", &query.Limit, 100}, {"offset", &query.Offset, 9007199254740991}} {
		if raw, present := values[parameter.name]; present {
			if !domain.PricingPageInteger.MatchString(raw[0]) {
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
		if !domain.PricingPageInteger.MatchString(raw[0]) {
			return query, fmt.Errorf("invalid creator_id")
		}
		number, err := strconv.ParseInt(raw[0], 10, 32)
		if err != nil {
			return query, fmt.Errorf("invalid creator_id")
		}
		id := int(number)
		query.CreatorID = &id
	}
	query.Search = domain.NormalizePricingSearch(values.Get("search"))
	for _, parameter := range []struct {
		name    string
		target  *string
		allowed []string
	}{
		{"catalog_type", &query.CatalogType, domain.CatalogModelTypes()},
		{"modality", &query.Modality, []string{"TEXT", "IMAGE", "VIDEO", "EMBEDDINGS", "RERANK", "OTHER"}},
		{"pricing_unit", &query.PricingUnit, []string{"TOKENS", "CALLS", "UNCONFIGURED"}},
		{"order_by", &query.OrderBy, []string{"NAME", "INPUT_PRICE", "OUTPUT_PRICE"}},
		{"direction", &query.Direction, []string{"asc", "desc"}},
	} {
		if raw, present := values[parameter.name]; present {
			if (!personal && parameter.name != "modality" && parameter.name != "catalog_type") || !slices.Contains(parameter.allowed, raw[0]) {
				return query, fmt.Errorf("invalid %s", parameter.name)
			}
			*parameter.target = raw[0]
		}
	}
	return query, nil
}

// ParseEffectivePricingModelQuery parses the model detail selector.
func ParseEffectivePricingModelQuery(values url.Values) (string, string, error) {
	for key := range values {
		if key != "model_name" && key != "expected_revision" {
			return "", "", fmt.Errorf("unsupported %s", key)
		}
	}
	names := values["model_name"]
	if len(names) != 1 || strings.TrimSpace(names[0]) == "" {
		return "", "", fmt.Errorf("model_name is required")
	}
	expected, err := ExpectedPricingRevision(values)
	return names[0], expected, err
}

// ParseCatalogueMode retains full-response query compatibility while requiring
// an explicit limit for page selectors.
func ParseCatalogueMode(raw string) (url.Values, bool, error) {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return nil, false, fmt.Errorf("invalid pricing query")
	}
	missing, err := ParseCatalogTypeMissing(values)
	if err != nil {
		return nil, false, err
	}
	if missing && !values.Has("limit") {
		return nil, false, fmt.Errorf("limit is required for pricing pagination")
	}
	if _, present := values["limit"]; present {
		return values, true, nil
	}
	for _, name := range []string{"offset", "search", "modality", "catalog_type", "creator_id", "using_group", "pricing_unit", "order_by", "direction"} {
		if _, present := values[name]; present {
			return nil, false, fmt.Errorf("limit is required for pricing pagination")
		}
	}
	return values, false, nil
}
