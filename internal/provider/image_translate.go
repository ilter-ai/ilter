package provider

import (
	"strings"

	"github.com/ilter-ai/ilter/internal/model"
)

// Image content blocks arrive in whichever wire format the inbound request
// used (OpenAI's image_url, or Anthropic's image/source), independent of
// which upstream the request ends up routed to. These helpers translate
// blocks into the target provider's shape; non-image blocks (text, tool_use,
// tool_result, ...) and already-correctly-shaped image blocks pass through
// unchanged, so translation is a safe no-op for same-family requests.

// translateContentToAnthropic converts OpenAI-style image_url blocks within a
// message's Content into Anthropic's image/source block shape.
func translateContentToAnthropic(content any) any {
	blocks, ok := content.([]any)
	if !ok {
		return content
	}
	out := make([]any, len(blocks))
	for i, b := range blocks {
		bm, ok := b.(map[string]any)
		if !ok || bm["type"] != "image_url" {
			out[i] = b
			continue
		}
		imageURL, _ := bm["image_url"].(map[string]any)
		url, _ := imageURL["url"].(string)
		out[i] = openAIImageURLToAnthropicBlock(url)
	}
	return out
}

func openAIImageURLToAnthropicBlock(url string) map[string]any {
	if mediaType, data, ok := parseDataURI(url); ok {
		return map[string]any{
			"type": "image",
			"source": map[string]any{
				"type":       "base64",
				"media_type": mediaType,
				"data":       data,
			},
		}
	}
	return map[string]any{
		"type": "image",
		"source": map[string]any{
			"type": "url",
			"url":  url,
		},
	}
}

// translateMessagesToOpenAI returns a copy of msgs with any Anthropic-style
// image/source content blocks converted into OpenAI's image_url shape.
func translateMessagesToOpenAI(msgs []model.Message) []model.Message {
	out := make([]model.Message, len(msgs))
	for i, m := range msgs {
		out[i] = m
		out[i].Content = translateContentToOpenAI(m.Content)
	}
	return out
}

func translateContentToOpenAI(content any) any {
	blocks, ok := content.([]any)
	if !ok {
		return content
	}
	out := make([]any, len(blocks))
	for i, b := range blocks {
		bm, ok := b.(map[string]any)
		if !ok || bm["type"] != "image" {
			out[i] = b
			continue
		}
		source, _ := bm["source"].(map[string]any)
		out[i] = anthropicImageBlockToOpenAI(source)
	}
	return out
}

func anthropicImageBlockToOpenAI(source map[string]any) map[string]any {
	var url string
	switch source["type"] {
	case "base64":
		mediaType, _ := source["media_type"].(string)
		data, _ := source["data"].(string)
		url = "data:" + mediaType + ";base64," + data
	case "url":
		url, _ = source["url"].(string)
	}
	return map[string]any{
		"type":      "image_url",
		"image_url": map[string]any{"url": url},
	}
}

func parseDataURI(url string) (mediaType, data string, ok bool) {
	const prefix = "data:"
	if !strings.HasPrefix(url, prefix) {
		return "", "", false
	}
	rest := url[len(prefix):]
	mediaType, data, ok = strings.Cut(rest, ";base64,")
	return mediaType, data, ok
}
