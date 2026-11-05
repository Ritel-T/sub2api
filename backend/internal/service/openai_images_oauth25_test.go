//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOAuthImage25FallbackPreservesRequest(t *testing.T) {
	for _, tc := range []struct {
		name, options, response string
		status, calls           int
	}{
		{"endpoint_missing", "", `{"detail":"Not Found"}`, 404, 2},
		{"method_missing", "", `{"detail":"Method Not Allowed"}`, 405, 2},
		{"model_missing", "", `{"error":{"code":"model_not_found","message":"Not Found"}}`, 404, 1},
		{"ambiguous", "", `{"error":{"message":"image request rejected"}}`, 404, 1},
		{"batch", `,"n":2`, `{"detail":"Not Found"}`, 404, 1},
		{"fidelity", `,"input_fidelity":"high"`, `{"detail":"Not Found"}`, 404, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(`{"model":"gpt-image-2.5","prompt":"draw","quality":"low","size":"1024x1024"` + tc.options + `}`)
			c, _ := newOpenAIImagesTestContext(t, body)
			calls := 0
			upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				calls++
				if calls == 1 {
					return &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.response))}, nil
				}
				require.Equal(t, "/backend-api/codex/responses", req.URL.Path)
				wire, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				require.Equal(t, "gpt-image-2.5", gjson.GetBytes(wire, "tools.0.model").String())
				require.Equal(t, "low", gjson.GetBytes(wire, "tools.0.quality").String())
				require.Equal(t, "1024x1024", gjson.GetBytes(wire, "tools.0.size").String())
				require.False(t, gjson.GetBytes(wire, "reasoning.effort").Exists())
				// Real upstream shape: the image is in output_item.done; completed has only usage.
				frames := "data: {\"type\":\"response.created\",\"response\":{\"tools\":[{\"type\":\"image_generation\",\"model\":\"gpt-image-2-codex\"}]}}\n\n" +
					"data: {\"type\":\"response.output_item.done\",\"item\":{\"id\":\"ig_25\",\"type\":\"image_generation_call\",\"result\":\"aGVsbG8=\",\"size\":\"1254x1254\"}}\n\n" +
					"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"tool_usage\":{\"image_gen\":{\"input_tokens\":24,\"output_tokens\":515,\"output_tokens_details\":{\"image_tokens\":515}}}}}\n\n"
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(frames))}, nil
			}}
			svc := newOpenAIImagesTestService(upstream)
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			result, err := svc.ForwardImages(context.Background(), c, directImagesTestAccount(), body, parsed, "")
			require.Equal(t, tc.calls, calls)
			if tc.calls == 1 {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, 1, result.ImageCount)
			require.Equal(t, 515, result.Usage.ImageOutputTokens)
			require.Equal(t, []string{"1254x1254"}, result.ImageOutputSizes)
			require.Equal(t, "gpt-image-2.5", result.UpstreamModel)
			require.Equal(t, "gpt-image-2-codex", result.UpstreamResponseModel)
		})
	}
}

func TestOAuthImage25BridgeEffortOnlyAffectsSelfBuiltAlias(t *testing.T) {
	for _, model := range []string{"gpt-image-2.5", "gpt-image-2", "gpt-image-2.5-flare"} {
		payload, err := buildOpenAIImagesResponsesRequest(&OpenAIImagesRequest{Prompt: "draw", N: 1, Quality: "medium"}, model)
		require.NoError(t, err)
		require.Equal(t, "medium", gjson.GetBytes(payload, "tools.0.quality").String())
		require.Equal(t, model, gjson.GetBytes(payload, "tools.0.model").String())
		require.Equal(t, model != "gpt-image-2.5", gjson.GetBytes(payload, "reasoning.effort").Exists())
	}
	request := map[string]any{"model": "gpt-5.6-luna", "reasoning": map[string]any{"effort": "medium"}, "tools": []any{map[string]any{"type": "image_generation", "model": "gpt-image-2.5"}}}
	normalizeOpenAIResponsesImageGenerationTools(request)
	normalizeOpenAIResponsesImageOnlyModel(request)
	require.Equal(t, "medium", request["reasoning"].(map[string]any)["effort"])
}

func TestOAuthImage25DirectActualSizeAndUsage(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"model":"gpt-image-2.5","prompt":"draw","size":"1024x1024","stream":%t}`, stream))
			c, rec := newOpenAIImagesTestContext(t, body)
			wire := `{"data":[{"b64_json":"aGVsbG8="}],"size":"1024x1024","usage":{"input_tokens":24,"output_tokens":515,"output_tokens_details":{"image_tokens":515}}}`
			if stream {
				wire = "data: {\"type\":\"image_generation.completed\",\"b64_json\":\"aGVsbG8=\",\"size\":\"1024x1024\",\"usage\":{\"input_tokens\":24,\"output_tokens\":515,\"output_tokens_details\":{\"image_tokens\":515}}}\n\n"
			}
			wire = strings.ReplaceAll(wire, "aGVsbG8=", encodeOpenAIImageTestPNG(t, 1254, 1254))
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(wire))}}
			svc := newOpenAIImagesTestService(upstream)
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			result, err := svc.ForwardImages(context.Background(), c, directImagesTestAccount(), body, parsed, "")
			require.NoError(t, err)
			require.Equal(t, 1, result.ImageCount)
			require.Equal(t, []string{"1254x1254"}, result.ImageOutputSizes)
			require.Equal(t, 515, result.Usage.ImageOutputTokens)
			require.Contains(t, rec.Body.String(), "1254x1254")
			require.Empty(t, result.UpstreamResponseModel, "a requested alias is not observed engine identity")
			require.NotContains(t, rec.Body.String(), `"model":"gpt-image-2.5"`)
		})
	}
}

func TestOAuthImage25DoesNotRetryUnknownOrAdapterErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		sendError  bool
		code       string
	}{
		{"transport", "", 0, true, "image_result_unknown"},
		{"empty", `{"data":[]}`, 200, false, "image_result_unknown"},
		{"server_http", `{"error":{"message":"internal server error"}}`, 500, false, "image_result_unknown"},
		{"invalid_json", `{`, 200, false, "image_result_unknown"},
		{"adapter_http", `{"error":{"type":"image_generation_user_error","code":"invalid_value","param":"complexity","message":"Complexity 'medium' is not supported for this image model."}}`, 400, false, "image_adapter_parameter_error"},
		{"adapter_sse", "data: {\"type\":\"error\",\"error\":{\"type\":\"image_generation_user_error\",\"code\":\"invalid_value\",\"param\":\"complexity\",\"message\":\"Complexity 'medium' is not supported for this image model.\"}}\n\n", 200, false, "image_adapter_parameter_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(`{"model":"gpt-image-2.5","prompt":"draw"}`)
			if tc.name == "adapter_sse" {
				body = []byte(`{"model":"gpt-image-2.5","prompt":"draw","stream":true}`)
			}
			c, rec := newOpenAIImagesTestContext(t, body)
			calls := 0
			upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				calls++
				if tc.sendError {
					return nil, errors.New("connection interrupted")
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"X-Request-Id": {"req_25_unknown"}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			}}
			svc := newOpenAIImagesTestService(upstream)
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			result, err := svc.ForwardImages(context.Background(), c, directImagesTestAccount(), body, parsed, "")
			require.Nil(t, result)
			require.Error(t, err)
			require.Equal(t, 1, calls)
			var terminal *OpenAIImagesUpstreamError
			require.ErrorAs(t, err, &terminal)
			require.Equal(t, tc.code, terminal.Code)
			require.False(t, IsOpenAIImagesRetryableUpstreamError(terminal))
			var failover *UpstreamFailoverError
			require.NotErrorAs(t, err, &failover)
			require.Equal(t, "false", rec.Header().Get("x-should-retry"))
			if tc.name == "adapter_sse" {
				require.Equal(t, 1, strings.Count(rec.Body.String(), "event: error"))
				require.NotContains(t, rec.Body.String(), "image_result_unknown")
			}
			if tc.name != "adapter_sse" {
				require.Equal(t, "upstream_error", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
			}
			if !tc.sendError {
				require.Equal(t, "req_25_unknown", terminal.UpstreamRequestID)
			}
		})
	}
}

func TestOAuthImage25KnownClientErrorsKeepTheirMeaning(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 429} {
		c, rec := newOpenAIImagesTestContext(t, nil)
		original := &OpenAIImagesUpstreamError{StatusCode: status, ErrorType: "invalid_request_error", Code: "original_error", Message: "original message"}
		err, handled := finishOpenAIImage25ForwardError(c, directImagesTestAccount(), nil, "gpt-image-2.5", original)
		require.False(t, handled)
		require.Same(t, original, err)
		require.Empty(t, rec.Body.String())
		require.Empty(t, rec.Header().Get("x-should-retry"))
	}
}

func TestOAuthImage25UnknownStreamDoesNotRepeat(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2.5","prompt":"draw","stream":true}`)
	c, rec := newOpenAIImagesTestContext(t, body)
	calls := 0
	upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"image_generation.in_progress\"}\n\n"))}, nil
	}}
	svc := newOpenAIImagesTestService(upstream)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	result, err := svc.ForwardImages(context.Background(), c, directImagesTestAccount(), body, parsed, "")
	require.Nil(t, result)
	require.Equal(t, 1, calls)
	var terminal *OpenAIImagesUpstreamError
	require.ErrorAs(t, err, &terminal)
	require.Equal(t, "image_result_unknown", terminal.Code)
	var failover *UpstreamFailoverError
	require.NotErrorAs(t, err, &failover)
	require.NotContains(t, rec.Body.String(), `"model":"gpt-image-2.5"`)
	events := parseOpenAIImageTestSSEEvents(rec.Body.String())
	event, found := findOpenAIImageTestSSEEvent(events, "error")
	require.True(t, found, "a committed stream must end with an explicit unknown error")
	require.Equal(t, "image_result_unknown", gjson.Get(event.Data, "error.code").String())
	require.Contains(t, gjson.Get(event.Data, "error.message").String(), "Do not automatically resubmit")
	require.Equal(t, 1, strings.Count(rec.Body.String(), "event: error"))
}

func TestOAuthImage25BridgeDoesNotInventResponseModel(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"model":"gpt-image-2.5","prompt":"draw","stream":%t}`, stream))
			c, rec := newOpenAIImagesTestContext(t, body)
			wire := "data: {\"type\":\"response.output_item.done\",\"item\":{\"id\":\"ig_bare\",\"type\":\"image_generation_call\",\"result\":\"aGVsbG8=\"}}\n\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[]}}\n\n"
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}}
			svc := newOpenAIImagesTestService(upstream)
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			result, err := svc.ForwardImages(withOpenAIImagesForceResponses(context.Background()), c, directImagesTestAccount(), body, parsed, "")
			require.NoError(t, err)
			require.Equal(t, 1, result.ImageCount)
			require.Empty(t, result.UpstreamResponseModel)
			require.NotContains(t, rec.Body.String(), `"model":"gpt-image-2.5"`)
		})
	}
}
