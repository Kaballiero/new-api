package common

import "testing"

func TestIsImageGenerationModel(t *testing.T) {
	generation := []string{
		"dall-e-3",
		"gpt-image-1",
		"gpt-image-2",
		"gpt-image-2.5-flare",
		"chatgpt-image-latest",
		"imagen-4.0-generate-001",
		"google/imagen-4.0-generate-001",
		"black-forest-labs/flux-3-image",
		"black-forest-labs/flux.2-pro",
		"bytedance-seed/seedream-5-0-pro",
		"bytedance-seed/seededit-3.0",
		"qwen/qwen-image-3-pro",
		"krea/krea-2-large",
		"sourceful/riverflow-v2.5-pro",
		"x-ai/grok-imagine-image-2.0",
		"recraft/recraft-v4.1",
		"microsoft/mai-image-2.6",
		"ideogram/ideogram-v3",
		"stability/stable-diffusion-3.5",
	}
	for _, name := range generation {
		if !IsImageGenerationModel(name) {
			t.Errorf("%s: expected an image generation model", name)
		}
	}

	chat := []string{
		"gpt-4o",
		"gpt-5-image",
		"gpt-5.4-image-2",
		"gemini-3-pro-image",
		"gemini-2.5-flash-image",
		"nano-banana-pro-preview",
		"claude-sonnet-4",
		"qwen3-vl-8b-thinking",
		"grok-4.3",
	}
	for _, name := range chat {
		if IsImageGenerationModel(name) {
			t.Errorf("%s: expected a chat model, upstream serves it through chat completions", name)
		}
	}
}
