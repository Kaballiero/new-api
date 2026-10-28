package domain

type TransportSettings struct {
	TaskPluginKey          string
	ForceFormat            bool
	ThinkingToContent      bool
	Proxy                  string
	PassThroughBodyEnabled bool
	SystemPrompt           string
	SystemPromptOverride   bool

	OpenRouterModelReserveUSD map[string]float64

	OpenRouterReservationSource map[string]string

	OpenRouterSonarResearchReserveUSD *float64

	HTTPProtocol string

	HTTP2ConnectionShards int
}
