package list_channel_models

// Model is one entry of the built-in OpenAI-compatible model list.
type Model struct {
	Id                     string   `json:"id"`
	Object                 string   `json:"object"`
	Created                int      `json:"created"`
	OwnedBy                string   `json:"owned_by"`
	SupportedEndpointTypes []string `json:"supported_endpoint_types"`
}

type Output = []Model
