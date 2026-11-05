//go:build unit

package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOAuthImage26NativeOptionsAndIdentity(t *testing.T) {
	for _, model := range []string{"gpt-image-2.6-sunburst", "gpt-image-2.6-flare"} {
		t.Run(model, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"model":%q,"prompt":"draw","quality":"high","size":"1024x1024","complexity":"low","output_format":"gif","frame_count":2,"frame_duration_ms":210}`, model))
			c, rec := newOpenAIImagesTestContext(t, body)
			upstream := &httpUpstreamRecorder{resp: openAIImagesJSONResponse()}
			svc := newOpenAIImagesTestService(upstream)
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			result, err := svc.ForwardImages(context.Background(), c, directImagesTestAccount(), body, parsed, "")
			require.NoError(t, err)
			require.Equal(t, "/backend-api/codex/images/generations", upstream.lastReq.URL.Path)
			require.Equal(t, model, gjson.GetBytes(upstream.lastBody, "model").String())
			require.Equal(t, "high", gjson.GetBytes(upstream.lastBody, "quality").String())
			require.Equal(t, "low", gjson.GetBytes(upstream.lastBody, "complexity").String())
			require.Equal(t, int64(2), gjson.GetBytes(upstream.lastBody, "frame_count").Int())
			require.Equal(t, int64(210), gjson.GetBytes(upstream.lastBody, "frame_duration_ms").Int())
			require.Empty(t, result.UpstreamResponseModel)
			require.NotContains(t, rec.Body.String(), `"model":"gpt-image-2.6`)
		})
	}
	require.False(t, usesCodexDirectImages("gpt-image-2.6"))
	require.Equal(t, "image/svg+xml", openAIImageOutputMIMEType("svg"))
	require.Equal(t, "image/gif", openAIImageOutputMIMEType("gif"))
}

func TestOAuthImage26NoFallbackOrUnknownReplay(t *testing.T) {
	for _, status := range []int{404, 500} {
		body := []byte(`{"model":"gpt-image-2.6-flare","prompt":"draw"}`)
		c, rec := newOpenAIImagesTestContext(t, body)
		calls := 0
		upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"detail":"Not Found"}`))}, nil
		}}
		svc := newOpenAIImagesTestService(upstream)
		parsed, err := svc.ParseOpenAIImagesRequest(c, body)
		require.NoError(t, err)
		_, err = svc.ForwardImages(context.Background(), c, directImagesTestAccount(), body, parsed, "")
		require.Error(t, err)
		require.Equal(t, 1, calls)
		if status == 500 {
			require.Equal(t, "false", rec.Header().Get("x-should-retry"))
			var terminal *OpenAIImagesUpstreamError
			require.ErrorAs(t, err, &terminal)
			require.Equal(t, "image_result_unknown", terminal.Code)
		}
	}
}

func TestImage26MultipartAndCapabilities(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range map[string]string{"model": "gpt-image-2.6-flare", "prompt": "edit", "complexity": "medium", "frame_count": "3", "frame_duration_ms": "200", "output_format": "gif"} {
		require.NoError(t, writer.WriteField(key, value))
	}
	f, err := writer.CreateFormFile("image", "input.png")
	require.NoError(t, err)
	_, err = f.Write([]byte("image"))
	require.NoError(t, err)
	f, err = writer.CreateFormFile("markup", "markup.png")
	require.NoError(t, err)
	_, err = f.Write([]byte("markup"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	parsed := &OpenAIImagesRequest{N: 1, Endpoint: openAIImagesEditsEndpoint}
	require.NoError(t, parseOpenAIImagesMultipartRequest(body.Bytes(), writer.FormDataContentType(), parsed))
	require.Equal(t, "medium", parsed.Complexity)
	require.Equal(t, 3, *parsed.FrameCount)
	require.True(t, parsed.HasMarkup)
	err = ValidateOpenAIImagesRequestCapabilities(directImagesTestAccount(), parsed, "")
	var invalid *OpenAIImagesRequestValidationError
	require.ErrorAs(t, err, &invalid)
	require.Equal(t, "markup", invalid.Param)
	api := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	require.NoError(t, ValidateOpenAIImagesRequestCapabilities(api, parsed, ""))
	for _, options := range []string{`"complexity":"max"`, `"output_format":"svg","stream":true`, `"output_format":"svg","n":2`, `"output_format":"gif","frame_count":17`, `"frame_count":2`, `"output_format":"gif","prompt_rewriter":false`} {
		req := &OpenAIImagesRequest{N: 1, Endpoint: openAIImagesGenerationsEndpoint}
		require.NoError(t, parseOpenAIImagesJSONRequest([]byte(`{"model":"gpt-image-2.6-flare","prompt":"draw",`+options+`}`), req))
		require.Error(t, ValidateOpenAIImagesRequestCapabilities(api, req, ""), options)
	}
}

func TestOAuthImage26RejectsMarkupBeforeNetwork(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2.6-flare","prompt":"edit","images":[{"image_url":"https://example.com/input.png"}],"markup":{"image_url":"https://example.com/markup.png"}}`)
	c, _ := newOpenAIImagesTestContext(t, body)
	c.Request.URL.Path = openAIImagesEditsEndpoint
	calls := 0
	upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		calls++
		return nil, fmt.Errorf("must not send")
	}}
	svc := newOpenAIImagesTestService(upstream)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	_, err = svc.ForwardImages(context.Background(), c, directImagesTestAccount(), body, parsed, "")
	var invalid *OpenAIImagesRequestValidationError
	require.ErrorAs(t, err, &invalid)
	require.Equal(t, "markup", invalid.Param)
	require.Zero(t, calls)
}

func TestImage26APIKeyRetainsNewOptions(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2.6-sunburst","prompt":"draw","complexity":"high","output_format":"svg","svg":{"style":"flat"},"safety_identifier":"synthetic-test"}`)
	upstream, rec, _ := forwardAPIKeyFormatTest(t, openAIImagesGenerationsEndpoint, "application/json", body, "application/json", `{"output_format":"svg","data":[{"b64_json":"PHN2Zy8+"}]}`, nil, "")
	require.JSONEq(t, string(body), string(upstream.lastBody))
	require.Contains(t, rec.Body.String(), "PHN2Zy8+")
	var multipartBody bytes.Buffer
	writer := multipart.NewWriter(&multipartBody)
	for key, value := range map[string]string{"model": "gpt-image-2.6-flare", "prompt": "edit", "complexity": "medium"} {
		require.NoError(t, writer.WriteField(key, value))
	}
	for _, name := range []string{"image", "markup"} {
		f, err := writer.CreateFormFile(name, name+".png")
		require.NoError(t, err)
		_, err = f.Write([]byte(name + "-payload"))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	upstream, _, _ = forwardAPIKeyFormatTest(t, openAIImagesEditsEndpoint, writer.FormDataContentType(), multipartBody.Bytes(), "application/json", `{"data":[{"b64_json":"aGVsbG8="}]}`, nil, "")
	_, params, err := mime.ParseMediaType(upstream.lastReq.Header.Get("Content-Type"))
	require.NoError(t, err)
	reader := multipart.NewReader(bytes.NewReader(upstream.lastBody), params["boundary"])
	got := map[string]string{}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		payload, err := io.ReadAll(part)
		require.NoError(t, err)
		got[part.FormName()] = string(payload)
	}
	require.Equal(t, map[string]string{"model": "gpt-image-2.6-flare", "prompt": "edit", "complexity": "medium", "image": "image-payload", "markup": "markup-payload"}, got)
}
