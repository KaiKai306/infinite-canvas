package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tigerowo/infinite-canvas/model"
)

// Same seven standardized JSON sources used by basketikun/infinite-canvas.
const promptRegistryBase = "https://raw.githubusercontent.com/yukkcat/image-prompts/main/dist/sources/"

var registryPromptSources = map[string]bool{
	"banana-prompt-quicker": true, "davidwu-gpt-image2-prompts": true, "freestylefly-gpt-image-2": true,
	"awesome-gpt-image": true, "awesome-gpt4o-image-prompts": true,
	"youmind-gpt-image-2": true, "youmind-nano-banana-pro": true,
}

type registryPrompt struct {
	ID                 string   `json:"id"`
	Title              string   `json:"title"`
	Prompt             string   `json:"prompt"`
	Description        string   `json:"description"`
	CoverURL           string   `json:"coverUrl"`
	ReferenceImageURLs []string `json:"referenceImageUrls"`
	Tags               []string `json:"tags"`
	Author             string   `json:"author"`
	SourceURL          string   `json:"sourceUrl"`
	CreatedAt          string   `json:"createdAt"`
	UpdatedAt          string   `json:"updatedAt"`
}

func fetchRegistryPrompts(category string) ([]model.Prompt, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	response, err := client.Get(promptRegistryBase + category + ".json")
	if err != nil {
		return nil, errors.New("提示词 JSON 来源暂时无法连接")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("提示词 JSON 拉取失败")
	}
	return readRegistryPrompts(category, io.LimitReader(response.Body, 16<<20))
}

func readRegistryPrompts(category string, reader io.Reader) ([]model.Prompt, error) {
	var source []registryPrompt
	if err := json.NewDecoder(reader).Decode(&source); err != nil {
		return nil, errors.New("提示词来源 JSON 格式无效，保留原有数据")
	}
	items := []model.Prompt{}
	seen := map[string]bool{}
	for _, raw := range source {
		if strings.TrimSpace(raw.Title) == "" || strings.TrimSpace(raw.Prompt) == "" {
			continue
		}
		id := strings.TrimSpace(raw.ID)
		if id == "" {
			sum := sha256.Sum256([]byte(raw.Title + "\n" + raw.Prompt))
			id = hex.EncodeToString(sum[:])
		}
		id = category + ":" + id
		if seen[id] {
			continue
		}
		seen[id] = true
		images := []string{}
		for _, sourceURL := range append([]string{raw.CoverURL}, raw.ReferenceImageURLs...) {
			if link := promptRemoteURL(sourceURL); link != "" && !containsPromptURL(images, link) {
				images = append(images, link)
			}
		}
		cover := "/logo.svg"
		if len(images) > 0 {
			cover = images[0]
		}
		preview := markdownPreview(images)
		if raw.Description != "" {
			preview += "\n\n" + raw.Description
		}
		if raw.Author != "" {
			preview += "\n\n作者：" + raw.Author
		}
		if link := promptRemoteURL(raw.SourceURL); link != "" {
			preview += "\n\n原始来源：" + link
		}
		items = append(items, model.Prompt{ID: id, Category: category, Title: strings.TrimSpace(raw.Title), Prompt: raw.Prompt, CoverURL: cover, Tags: append([]string{}, raw.Tags...), Preview: preview, CreatedAt: raw.CreatedAt, UpdatedAt: firstNonEmpty(raw.UpdatedAt, raw.CreatedAt)})
	}
	if len(items) == 0 {
		return nil, errors.New("提示词来源没有有效条目，保留原有数据")
	}
	return items, nil
}

func promptRemoteURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return ""
	}
	return parsed.String()
}

func containsPromptURL(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
