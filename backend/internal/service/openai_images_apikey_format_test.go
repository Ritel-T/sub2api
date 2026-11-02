//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Exercise parsing and the public forwarding entrypoint with a captured upstream
// request, so format adaptation is checked on both sides of the gateway.
func forwardAPIKeyFormatTest(t *testing.T, endpoint, contentType string, body []byte, responseType, response string, mapping map[string]any, channelModel string) (*httpUpstreamRecorder, *httptest.ResponseRecorder, *OpenAIForwardResult) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", contentType)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {responseType}, "X-Request-Id": {"req_apikey_format"}},
		Body:       io.NopCloser(strings.NewReader(response)),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := &Account{ID: 7, Name: "image-format-test", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "test-key", "base_url": "https://image-upstream.example/v1", "model_mapping": mapping},
		// This provider alias uses the existing explicit image-model grant.
		Extra: map[string]any{ExtraImageModelsKey: []string{"chatgpt-image-latest"}},
	}
	c.Set("api_key", &APIKey{})
	parser := &OpenAIGatewayService{accountRepo: &imageAuthorizationRepo{accounts: []Account{*account}}}
	parsed, err := parser.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, channelModel)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "https://image-upstream.example"+endpoint, upstream.lastReq.URL.String())
	require.Equal(t, "Bearer test-key", upstream.lastReq.Header.Get("Authorization"))
	require.Len(t, upstream.requests, 1)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "req_apikey_format", result.RequestID)
	require.Equal(t, 1, result.ImageCount)
	return upstream, rec, result
}

func TestOpenAIImagesAPIKeyFormat_JSON(t *testing.T) {
	for _, tc := range []struct {
		name, model, format, requestOutput, itemOutput, topOutput, wantMIME string
		edit                                                                bool
	}{
		{name: "generation_url_item_format_wins", model: "gpt-image-2", format: "url", requestOutput: "png", itemOutput: "webp", topOutput: "jpeg", wantMIME: "image/webp"},
		{name: "generation_url_top_format", model: "gpt-image-1", format: "url", requestOutput: "png", topOutput: "jpeg", wantMIME: "image/jpeg"},
		{name: "generation_url_request_fallback", model: "chatgpt-image-latest", format: "url", requestOutput: "webp", wantMIME: "image/webp"},
		{name: "generation_url_default_png", model: "gpt-image-2", format: "url", wantMIME: "image/png"},
		{name: "generation_b64", model: "gpt-image-2", format: "b64_json", requestOutput: "jpeg"},
		{name: "generation_default_b64", model: "gpt-image-2"},
		{name: "edit_url", model: "gpt-image-2", format: "url", requestOutput: "jpeg", wantMIME: "image/jpeg", edit: true},
		{name: "non_gpt_url_unchanged", model: "grok-imagine", format: "url", requestOutput: "webp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := map[string]any{"model": tc.model, "prompt": "replace the background", "n": 1, "size": "1024x1024", "quality": "high", "background": "transparent", "output_compression": 81, "user": "format-regression", "custom_field": map[string]any{"keep": true}}
			if tc.format != "" {
				request["response_format"] = tc.format
			}
			if tc.requestOutput != "" {
				request["output_format"] = tc.requestOutput
			}
			endpoint := openAIImagesGenerationsEndpoint
			if tc.edit {
				endpoint = openAIImagesEditsEndpoint
				request["images"] = []any{map[string]any{"image_url": "https://example.com/source.png"}}
				request["mask"] = map[string]any{"image_url": "https://example.com/mask.png"}
				request["input_fidelity"] = "high"
			}
			body, err := json.Marshal(request)
			require.NoError(t, err)
			item := map[string]any{"b64_json": "aGVsbG8=", "revised_prompt": "a blue background"}
			if tc.itemOutput != "" {
				item["output_format"] = tc.itemOutput
			}
			response := map[string]any{"created": 1710000007, "data": []any{item}, "usage": map[string]any{"input_tokens": 12, "output_tokens": 21, "output_tokens_details": map[string]any{"image_tokens": 9}}}
			if tc.topOutput != "" {
				response["output_format"] = tc.topOutput
			}
			wire, err := json.Marshal(response)
			require.NoError(t, err)
			upstream, rec, result := forwardAPIKeyFormatTest(t, endpoint, "application/json", body, "application/json", string(wire), nil, "")
			expected := make(map[string]any, len(request))
			for k, v := range request {
				expected[k] = v
			}
			if tc.model != "grok-imagine" {
				delete(expected, "response_format")
			}
			expectedJSON, err := json.Marshal(expected)
			require.NoError(t, err)
			// Whole-object equality catches loss of edit inputs and unknown options.
			require.JSONEq(t, string(expectedJSON), string(upstream.lastBody))
			expectedResponse := string(wire)
			if tc.wantMIME != "" {
				delete(item, "b64_json")
				item["url"] = "data:" + tc.wantMIME + ";base64,aGVsbG8="
				adapted, err := json.Marshal(response)
				require.NoError(t, err)
				expectedResponse = string(adapted)
			}
			require.JSONEq(t, expectedResponse, rec.Body.String())
			require.Equal(t, 12, result.Usage.InputTokens)
			require.Equal(t, 21, result.Usage.OutputTokens)
			require.Equal(t, 9, result.Usage.ImageOutputTokens)
		})
	}
}

func TestOpenAIImagesAPIKeyFormat_MappedModel(t *testing.T) {
	for _, tc := range []struct {
		name, client, channel, upstream string
		gpt                             bool
	}{
		{"account_mapping_to_gpt", "grok-imagine", "", "gpt-image-2", true},
		{"channel_then_account_to_latest", "grok-imagine", "gemini-3-pro-image", "chatgpt-image-latest", true},
		{"gpt_mapping_to_non_gpt", "gpt-image-2", "", "grok-imagine", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"model": tc.client, "prompt": "a cat", "response_format": "url"})
			require.NoError(t, err)
			source := tc.client
			if tc.channel != "" {
				source = tc.channel
			}
			upstream, rec, result := forwardAPIKeyFormatTest(t, openAIImagesGenerationsEndpoint, "application/json", body, "application/json", `{"data":[{"b64_json":"aGVsbG8="}]}`, map[string]any{source: tc.upstream}, tc.channel)
			require.Equal(t, tc.upstream, gjson.GetBytes(upstream.lastBody, "model").String())
			require.Equal(t, tc.upstream, result.UpstreamModel)
			if tc.gpt {
				require.False(t, gjson.GetBytes(upstream.lastBody, "response_format").Exists())
				require.Equal(t, "data:image/png;base64,aGVsbG8=", gjson.Get(rec.Body.String(), "data.0.url").String())
				require.False(t, gjson.Get(rec.Body.String(), "data.0.b64_json").Exists())
			} else {
				require.Equal(t, "url", gjson.GetBytes(upstream.lastBody, "response_format").String())
				require.JSONEq(t, `{"data":[{"b64_json":"aGVsbG8="}]}`, rec.Body.String())
			}
		})
	}
}

func TestOpenAIImagesAPIKeyFormat_MultipartEdit(t *testing.T) {
	type part struct {
		header textproto.MIMEHeader
		body   []byte
	}
	for _, model := range []string{"gpt-image-2", "grok-imagine"} {
		t.Run(model, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			fields := [][2]string{{"model", model}, {"prompt", "edit both images"}, {"response_format", "url"}, {"response_format", "url"}, {"output_format", "webp"}, {"quality", "high"}, {"custom_field", "first"}, {"custom_field", "second"}}
			expected := make([]part, 0, len(fields)+3)
			for _, field := range fields {
				require.NoError(t, writer.WriteField(field[0], field[1]))
				if field[0] != "response_format" || model == "grok-imagine" {
					expected = append(expected, part{textproto.MIMEHeader{"Content-Disposition": {`form-data; name="` + field[0] + `"`}}, []byte(field[1])})
				}
			}
			// Repeated image[] uploads plus a mask must retain their headers and bytes.
			for _, file := range []struct {
				field, name, kind string
				data              []byte
			}{
				{"image[]", "source-one.png", "image/png", []byte{0, 1, 2, 13, 10, 255}},
				{"image[]", "source-two.jpg", "image/jpeg", []byte{255, 216, 0, 42}},
				{"mask", "mask.png", "image/png", []byte{0, 255, 0, 13, 10}},
			} {
				header := textproto.MIMEHeader{"Content-Disposition": {`form-data; name="` + file.field + `"; filename="` + file.name + `"`}, "Content-Type": {file.kind}, "X-Upload-Metadata": {"keep-me"}}
				target, err := writer.CreatePart(header)
				require.NoError(t, err)
				_, err = target.Write(file.data)
				require.NoError(t, err)
				expected = append(expected, part{header, file.data})
			}
			require.NoError(t, writer.Close())
			upstream, rec, _ := forwardAPIKeyFormatTest(t, openAIImagesEditsEndpoint, writer.FormDataContentType(), body.Bytes(), "application/json", `{"data":[{"b64_json":"aGVsbG8="}]}`, nil, "")
			mediaType, params, err := mime.ParseMediaType(upstream.lastReq.Header.Get("Content-Type"))
			require.NoError(t, err)
			require.Equal(t, "multipart/form-data", mediaType)
			reader := multipart.NewReader(bytes.NewReader(upstream.lastBody), params["boundary"])
			var actual []part
			for {
				p, err := reader.NextPart()
				if err == io.EOF {
					break
				}
				require.NoError(t, err)
				payload, err := io.ReadAll(p)
				require.NoError(t, err)
				actual = append(actual, part{p.Header, payload})
				require.NoError(t, p.Close())
			}
			require.Equal(t, expected, actual)
			if model == "gpt-image-2" {
				require.JSONEq(t, `{"data":[{"url":"data:image/webp;base64,aGVsbG8="}]}`, rec.Body.String())
			} else {
				require.JSONEq(t, `{"data":[{"b64_json":"aGVsbG8="}]}`, rec.Body.String())
			}
		})
	}
}

func TestOpenAIImagesAPIKeyFormat_SSE(t *testing.T) {
	for _, tc := range []struct{ name, model, format string }{
		{"gpt_url", "gpt-image-2", "url"},
		{"latest_url", "chatgpt-image-latest", "url"},
		{"gpt_b64_passthrough", "gpt-image-2", "b64_json"},
		{"gpt_default_passthrough", "gpt-image-2", ""},
		{"non_gpt_url_passthrough", "grok-imagine", "url"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := map[string]any{"model": tc.model, "prompt": "a cat", "stream": true, "output_format": "webp"}
			if tc.format != "" {
				request["response_format"] = tc.format
			}
			body, err := json.Marshal(request)
			require.NoError(t, err)
			// Preview uses the request output format; completion overrides it to jpeg.
			// The progress event has no image payload and must remain payload-free.
			wire := "event: image_generation.in_progress\ndata: {\"type\":\"image_generation.in_progress\"}\n\n" +
				"event: image_generation.partial_image\ndata: {\"type\":\"image_generation.partial_image\",\"partial_image_index\":0,\"b64_json\":\"cHJldmlldw==\"}\n\n" +
				"event: image_generation.completed\ndata: {\"type\":\"image_generation.completed\",\"b64_json\":\"aGVsbG8=\",\"output_format\":\"jpeg\",\"usage\":{\"input_tokens\":12,\"output_tokens\":21}}\n\n" +
				"data: [DONE]\n\n"
			upstream, rec, result := forwardAPIKeyFormatTest(t, openAIImagesGenerationsEndpoint, "application/json", body, "text/event-stream", wire, nil, "")
			require.True(t, result.Stream)
			require.Equal(t, 12, result.Usage.InputTokens)
			require.Equal(t, 21, result.Usage.OutputTokens)
			require.Zero(t, result.Usage.ImageOutputTokens, "response format must not change API key usage classification")
			require.Contains(t, rec.Body.String(), "data: [DONE]\n\n")
			require.Contains(t, rec.Header().Get("Content-Type"), "text/event-stream")
			if tc.model == "grok-imagine" {
				require.Equal(t, "url", gjson.GetBytes(upstream.lastBody, "response_format").String())
			} else {
				require.False(t, gjson.GetBytes(upstream.lastBody, "response_format").Exists())
			}
			if tc.format != "url" || tc.model == "grok-imagine" {
				require.Equal(t, wire, rec.Body.String())
				return
			}
			var events []gjson.Result
			for _, line := range strings.Split(rec.Body.String(), "\n") {
				if strings.HasPrefix(line, "data: ") && gjson.Valid(strings.TrimPrefix(line, "data: ")) {
					events = append(events, gjson.Parse(strings.TrimPrefix(line, "data: ")))
				}
			}
			require.Len(t, events, 3)
			require.Equal(t, "image_generation.in_progress", events[0].Get("type").String())
			require.False(t, events[0].Get("url").Exists())
			require.False(t, events[0].Get("b64_json").Exists())
			require.Equal(t, "data:image/webp;base64,cHJldmlldw==", events[1].Get("url").String())
			require.Equal(t, "cHJldmlldw==", events[1].Get("b64_json").String())
			require.Equal(t, int64(0), events[1].Get("partial_image_index").Int())
			require.Equal(t, "data:image/jpeg;base64,aGVsbG8=", events[2].Get("url").String())
			require.Equal(t, "aGVsbG8=", events[2].Get("b64_json").String())
			require.Contains(t, rec.Body.String(), "event: image_generation.completed")
		})
	}
}
