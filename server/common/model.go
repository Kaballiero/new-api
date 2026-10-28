package common

import "strings"

var (
	// OpenAIResponseOnlyModels is a list of models that are only available for OpenAI responses.
	OpenAIResponseOnlyModels = []string{
		"o3-pro",
		"o3-deep-research",
		"o4-mini-deep-research",
	}
	// ImageGenerationModels matches models served through the image generation
	// endpoint. Entries are matched as substrings of the lowercased model name, so
	// a family name covers its versions and vendor-prefixed aliases alike
	// ("flux" covers "flux.2-pro" and "black-forest-labs/flux-3-image"). Chat
	// models that merely return images, such as gemini-*-image or gpt-5-image, are
	// deliberately absent: upstream serves those through chat completions.
	ImageGenerationModels = []string{
		"dall-e",
		"gpt-image",
		"imagen-",
		"flux",
		"seedream",
		"seededit",
		"qwen-image",
		"krea",
		"riverflow",
		"grok-imagine",
		"recraft",
		"mai-image",
		"ideogram",
		"stable-diffusion",
	}
	OpenAITextModels = []string{
		"gpt-",
		"o1",
		"o3",
		"o4",
		"chatgpt",
	}
)

func IsOpenAIResponseOnlyModel(modelName string) bool {
	for _, m := range OpenAIResponseOnlyModels {
		if strings.Contains(modelName, m) {
			return true
		}
	}
	return false
}

func IsImageGenerationModel(modelName string) bool {
	modelName = strings.ToLower(modelName)
	for _, m := range ImageGenerationModels {
		if strings.Contains(modelName, m) {
			return true
		}
		if strings.HasPrefix(m, "prefix:") && strings.HasPrefix(modelName, strings.TrimPrefix(m, "prefix:")) {
			return true
		}
	}
	return false
}

func IsOpenAITextModel(modelName string) bool {
	modelName = strings.ToLower(modelName)
	for _, m := range OpenAITextModels {
		if strings.Contains(modelName, m) {
			return true
		}
	}
	return false
}
