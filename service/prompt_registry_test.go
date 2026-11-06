package service

import (
	"strings"
	"testing"
)

func TestRegistryPromptsKeepExternalImagesAndProvenance(t *testing.T) {
	items, err := readRegistryPrompts("example", strings.NewReader(`[
 {"id":"one","title":"示例","prompt":"画一只猫","coverUrl":"data:image/png;base64,AAAA","referenceImageUrls":["https://images.example/cat.png","https://images.example/cat.png"],"author":"<author>","sourceUrl":"https://example.com/source","tags":["动物"]},
 {"id":"one","title":"重复","prompt":"duplicate"},
 {"title":"无图","prompt":"still usable","coverUrl":"javascript:alert(1)"},
 {"title":"invalid","prompt":""}
 ]`))
	if err != nil || len(items) != 2 {
		t.Fatalf("invalid registry parsing: count=%d error=%v", len(items), err)
	}
	if items[0].CoverURL != "https://images.example/cat.png" || !strings.Contains(items[0].Preview, "作者：<author>") || !strings.Contains(items[0].Preview, "原始来源：https://example.com/source") {
		t.Fatal("external image or provenance lost")
	}
	if strings.Contains(items[0].Preview, "data:image") || strings.Contains(items[1].Preview, "javascript:") || items[1].Tags == nil {
		t.Fatal("unsafe URL or null tags admitted")
	}
	if _, err := readRegistryPrompts("example", strings.NewReader(`[]`)); err == nil {
		t.Fatal("empty source would erase prior library")
	}
	if _, err := readRegistryPrompts("example", strings.NewReader(`{"not":"an array"}`)); err == nil {
		t.Fatal("invalid schema accepted")
	}
}
