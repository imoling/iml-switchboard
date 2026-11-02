package core

import "testing"

func TestGuessMediaKind(t *testing.T) {
	for id, want := range map[string]string{"gpt-image-2": "image", "gpt-image-2.5-flare": "image", "dall-e-3": "image", "sora-2": "video", "veo-3": "video",
		"gpt-6.1-sol": "", "gpt-4o-audio-preview": "", "gpt-6-chat-thinking": ""} {
		if got := GuessMediaKind(id); got != want {
			t.Errorf("%s：%q，应为 %q", id, got, want)
		}
	}
}
