package integration

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

type toolImage struct {
	Path      string
	MediaType string
	Data      string
}

// decodeToolResult keeps ordinary tool results byte-for-byte compatible. The
// image envelope is decoded only when the read tool explicitly returns images.
func decodeToolResult(raw string) (string, []toolImage) {
	var envelope struct {
		Content string `json:"content"`
		Images  []struct {
			Path      string `json:"path"`
			MediaType string `json:"media_type"`
			Encoding  string `json:"encoding"`
			Data      string `json:"data"`
		} `json:"images"`
	}
	if json.Unmarshal([]byte(raw), &envelope) != nil || len(envelope.Images) == 0 {
		return raw, nil
	}
	images := make([]toolImage, 0, len(envelope.Images))
	for _, image := range envelope.Images {
		if image.Encoding != "base64" || image.Data == "" || !strings.HasPrefix(image.MediaType, "image/") {
			continue
		}
		if _, err := base64.StdEncoding.DecodeString(image.Data); err != nil {
			continue
		}
		images = append(images, toolImage{Path: image.Path, MediaType: image.MediaType, Data: image.Data})
	}
	if len(images) == 0 {
		return raw, nil
	}
	if envelope.Content == "" {
		envelope.Content = fmt.Sprintf("The read tool returned %d image attachment(s).", len(images))
	}
	return envelope.Content, images
}

func imageDataURL(image toolImage) string {
	return "data:" + image.MediaType + ";base64," + image.Data
}

func anthropicToolResultContent(raw string) any {
	text, images := decodeToolResult(raw)
	if len(images) == 0 {
		return raw
	}
	blocks := make([]any, 0, len(images)+1)
	if text != "" {
		blocks = append(blocks, map[string]any{"type": "text", "text": text})
	}
	for _, image := range images {
		blocks = append(blocks, map[string]any{
			"type": "image",
			"source": map[string]any{
				"type":       "base64",
				"media_type": image.MediaType,
				"data":       image.Data,
			},
		})
	}
	return blocks
}
