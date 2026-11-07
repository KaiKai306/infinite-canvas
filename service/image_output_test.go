package service

import (
	"github.com/tigerowo/infinite-canvas/model"
	"testing"
)

func TestImageAPIGIFParameters(t *testing.T) {
	credential := model.ImageAPICredential{Model: "gpt-image-2.6-flare"}
	input := ImageAPIJobInput{Prompt: "animated scene", OutputFormat: "gif"}
	normalized, err := NormalizeImageAPIJobInput(input, credential)
	if err != nil || normalized.FrameCount != 4 || normalized.FrameDurationMs != 200 {
		t.Fatal("GIF defaults not normalized")
	}
	for _, modelName := range []string{"gpt-image-2", "gpt-image-2.5-flare", "other-image"} {
		bad := input
		bad.Model = modelName
		if _, err := NormalizeImageAPIJobInput(bad, credential); err == nil {
			t.Fatal("unsupported GIF model accepted")
		}
	}
	for _, bad := range []ImageAPIJobInput{
		{Prompt: "p", OutputFormat: "gif", FrameCount: 17},
		{Prompt: "p", OutputFormat: "gif", FrameDurationMs: 19},
		{Prompt: "p", FrameCount: 4},
		{Prompt: "p", OutputFormat: "svg"},
	} {
		if _, err := NormalizeImageAPIJobInput(bad, credential); err == nil {
			t.Fatal("invalid output parameters accepted")
		}
	}
	plain, err := NormalizeImageAPIJobInput(ImageAPIJobInput{Prompt: "p"}, credential)
	explicit, otherErr := NormalizeImageAPIJobInput(ImageAPIJobInput{Prompt: "p", OutputFormat: "png"}, credential)
	if err != nil || otherErr != nil || plain != explicit {
		t.Fatal("PNG idempotency changed")
	}
}
