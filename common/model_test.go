package common

import "testing"

func TestIsAudioSpeechModel(t *testing.T) {
	for _, m := range []string{"tts-1", "tts-1-hd", "gpt-4o-mini-tts", "TTS-1"} {
		if !IsAudioSpeechModel(m) {
			t.Errorf("IsAudioSpeechModel(%q) = false, want true", m)
		}
	}
	for _, m := range []string{"gpt-4o", "whisper-1", "dall-e-3", ""} {
		if IsAudioSpeechModel(m) {
			t.Errorf("IsAudioSpeechModel(%q) = true, want false", m)
		}
	}
}

func TestIsImageGenerationModel(t *testing.T) {
	for _, m := range []string{"dall-e-3", "gpt-image-1", "imagen-3.0", "flux-pro", "seedream-3.0"} {
		if !IsImageGenerationModel(m) {
			t.Errorf("IsImageGenerationModel(%q) = false, want true", m)
		}
	}
	for _, m := range []string{"gpt-4o", "tts-1", ""} {
		if IsImageGenerationModel(m) {
			t.Errorf("IsImageGenerationModel(%q) = true, want false", m)
		}
	}
}
