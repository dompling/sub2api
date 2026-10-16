package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestLeonardoImagesStreamEventsPerImage(t *testing.T) {
	result := &LeonardoImageResult{
		Body:         []byte(`{"created":1700000000,"data":[{"b64_json":"QUJD"},{"b64_json":"REVG"}]}`),
		Size:         "1024x1024",
		OutputFormat: "png",
		Quality:      "xhigh",
		Background:   "transparent",
	}
	events := LeonardoImagesStreamEvents(&OpenAIImagesRequest{Model: "gpt-image-2.5-flare", Stream: true}, result)
	require.Len(t, events, 2)
	for i, want := range []string{"QUJD", "REVG"} {
		event := events[i]
		require.Equal(t, "image_generation.completed", event.Name)
		require.Equal(t, "image_generation.completed", gjson.GetBytes(event.Payload, "type").String())
		require.Equal(t, want, gjson.GetBytes(event.Payload, "b64_json").String())
		require.EqualValues(t, 1700000000, gjson.GetBytes(event.Payload, "created_at").Int())
		require.Equal(t, "1024x1024", gjson.GetBytes(event.Payload, "size").String())
		require.Equal(t, "png", gjson.GetBytes(event.Payload, "output_format").String())
		require.Equal(t, "xhigh", gjson.GetBytes(event.Payload, "quality").String())
		require.Equal(t, "transparent", gjson.GetBytes(event.Payload, "background").String())
		require.Equal(t, "gpt-image-2.5-flare", gjson.GetBytes(event.Payload, "model").String())
		require.False(t, gjson.GetBytes(event.Payload, "url").Exists())
	}
}

func TestLeonardoImagesStreamEventsEditsAndURLs(t *testing.T) {
	// 编辑请求用 image_edit 前缀；response_format=url 时附 data URI。
	edit := LeonardoImagesStreamEvents(&OpenAIImagesRequest{Endpoint: openAIImagesEditsEndpoint, ResponseFormat: "url"}, &LeonardoImageResult{
		Body:         []byte(`{"data":[{"b64_json":"QUJD"}]}`),
		OutputFormat: "jpeg",
	})
	require.Len(t, edit, 1)
	require.Equal(t, "image_edit.completed", edit[0].Name)
	require.Equal(t, "data:image/jpeg;base64,QUJD", gjson.GetBytes(edit[0].Payload, "url").String())

	// 对象存储转存后只有 url：b64_json 留空，url 填存储地址；空条目跳过。
	stored := LeonardoImagesStreamEvents(&OpenAIImagesRequest{}, &LeonardoImageResult{
		Body: []byte(`{"data":[{"url":"https://cdn.example.com/a.png"},{}]}`),
	})
	require.Len(t, stored, 1)
	require.Equal(t, "", gjson.GetBytes(stored[0].Payload, "b64_json").String())
	require.Equal(t, "https://cdn.example.com/a.png", gjson.GetBytes(stored[0].Payload, "url").String())

	require.Nil(t, LeonardoImagesStreamEvents(&OpenAIImagesRequest{}, nil))
}

func TestImagesStreamErrorPayload(t *testing.T) {
	payload := ImagesStreamErrorPayload("leonardo_provider_moderation", "Content flagged by provider moderation")
	require.Equal(t, "error", gjson.GetBytes(payload, "type").String())
	require.Equal(t, "leonardo_provider_moderation", gjson.GetBytes(payload, "error.type").String())
	require.Equal(t, "Content flagged by provider moderation", gjson.GetBytes(payload, "error.message").String())
}

func TestValidateLeonardoImageParamsAcceptsStreamAndPartialImages(t *testing.T) {
	partials := 2
	require.NoError(t, validateLeonardoImageParams(&OpenAIImagesRequest{Prompt: "cat", Stream: true, PartialImages: &partials}))
	require.Error(t, validateLeonardoImageParams(&OpenAIImagesRequest{Prompt: "cat", HasMask: true}))
	require.Error(t, validateLeonardoImageParams(&OpenAIImagesRequest{Prompt: "cat", N: 9}))
}

// 出图结果带上假流式事件需要的元数据：实际尺寸、按文件头判定的格式、质量、背景。
func TestLeonardoGenerateCallResultMetadata(t *testing.T) {
	upstream := newFakeLeonardoUpstream(t)
	account := &Account{ID: 86, Platform: PlatformLeonardo, Type: AccountTypeOAuth, Credentials: map[string]any{"cookie": "c"}}
	result, err := upstream.imageService().GenerateCall(context.Background(), account, "jwt", NewLeonardoImageCall(&OpenAIImagesRequest{
		Model: "gpt-image-2", Prompt: "an apple", Quality: "low", N: 2, Stream: true,
	}, ""))
	require.NoError(t, err)
	require.Equal(t, "1024x1024", result.Size)
	require.Equal(t, "jpeg", result.OutputFormat)
	require.Equal(t, "low", result.Quality)
	require.Equal(t, "opaque", result.Background)
	require.Len(t, LeonardoImagesStreamEvents(&OpenAIImagesRequest{Stream: true}, result), 2)
}
