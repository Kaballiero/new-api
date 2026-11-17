package domain

// ReserveSource is what the reserve of an OpenRouter request is priced from;
// the admin log records it as openrouter_reservation_source.
type ReserveSource string

const (
	// ReserveSourceCatalog is the fresh upstream catalog.
	ReserveSourceCatalog ReserveSource = "catalog"
	// ReserveSourceCatalogStale is a stale catalog document taken because the
	// live endpoints failed.
	ReserveSourceCatalogStale ReserveSource = "catalog_stale"
	// ReserveSourceLive is the endpoints OpenRouter publishes at request time.
	ReserveSourceLive ReserveSource = "live"
	// ReserveSourceConfiguredPrice is the configured local price of the channel
	// model.
	ReserveSourceConfiguredPrice ReserveSource = "configured_price"
	// ReserveSourceManual is the approximate reserve of the channel model.
	ReserveSourceManual ReserveSource = "manual"
)

// ModelTariff takes the tariff a model is priced from in the order of the
// reserve sources: its catalog document while the catalog is fresh, then the
// tariff live discovers, then a stale document when live fails. err is the
// live failure: beside a stale document, or alone with an empty source when
// the model has no document.
func ModelTariff[T any](model string, catalog map[string]T, fresh bool, live interface{ Discover(string) (T, error) }) (T, ReserveSource, error) {
	document, documented := catalog[model]
	if documented && fresh {
		return document, ReserveSourceCatalog, nil
	}
	tariff, err := live.Discover(model)
	if err == nil {
		return tariff, ReserveSourceLive, nil
	}
	if !documented {
		var none T
		return none, "", err
	}
	return document, ReserveSourceCatalogStale, err
}

// AdmissionPolicy is the channel reserve policy the gateway applies to an
// OpenRouter request; the admin log records it as openrouter_admission_policy.
type AdmissionPolicy string

const (
	// AdmissionPolicyModelApproximateReserve holds the approximate reserve of
	// the channel model whole.
	AdmissionPolicyModelApproximateReserve AdmissionPolicy = "model_approximate_reserve"
	// AdmissionPolicyModelReserveAddOn adds the reserve of the channel model to
	// the endpoints that price hosted search.
	AdmissionPolicyModelReserveAddOn AdmissionPolicy = "model_reserve_add_on"
	// AdmissionPolicySonarResearchApproximateReserve holds the approximate
	// reserve of perplexity/sonar-deep-research whole.
	AdmissionPolicySonarResearchApproximateReserve AdmissionPolicy = "sonar_research_approximate_reserve"
)
