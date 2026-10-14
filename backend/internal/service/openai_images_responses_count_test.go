package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// The real provider rejects tools[0].n. A batch must fail before sending rather
// than silently turning n=2 into one generated and billed image.
func TestResponsesImagesBatchRejectsBeforeUpstream(t *testing.T) {
	for _, tc := range []struct{ name, requested, channel, mapped string }{
		{"bare_generation", "gpt-image-2.5", "", ""},
		{"account_mapping_to_responses", "gpt-image-2", "", "gpt-image-2.5"},
		{"channel_mapping_to_responses", "gpt-image-2", "gpt-image-2.5", ""},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", tc.name, stream), func(t *testing.T) {
				body := []byte(fmt.Sprintf(`{"model":%q,"prompt":"draw a circle","n":2,"stream":%t}`, tc.requested, stream))
				c, rec := newOpenAIImagesTestContext(t, body)
				calls := 0
				upstream := &codexModelsHTTPUpstreamStub{do: func(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
					calls++
					return &http.Response{StatusCode: 400, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"type":"invalid_request_error","code":"unknown_parameter","param":"tools[0].n","message":"Unknown parameter: 'tools[0].n'."}}`))}, nil
				}}
				svc := newOpenAIImagesTestService(upstream)
				account := directImagesTestAccount()
				if tc.mapped != "" {
					account.Credentials["model_mapping"] = map[string]any{tc.requested: tc.mapped}
				}
				parsed, err := svc.ParseOpenAIImagesRequest(c, body)
				require.NoError(t, err)
				result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, tc.channel)
				require.Nil(t, result)
				var countErr *OpenAIImagesRequestValidationError
				require.ErrorAs(t, err, &countErr)
				require.Zero(t, calls, "unsupported batch must not reach the provider")
				require.Equal(t, 200, rec.Code)
				require.Empty(t, rec.Body.String(), "handler owns the validation error response")
				require.Equal(t, "unsupported_image_count", countErr.Code)
				require.Equal(t, "n", countErr.Param)
				var failover *UpstreamFailoverError
				require.NotErrorAs(t, err, &failover)

				_, hasOps := c.Get(OpsUpstreamErrorsKey)
				require.False(t, hasOps)
			})
		}
	}
}

func TestResponsesImagesBatchRejectsBeforeCredentials(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2.5","prompt":"draw","n":2}`)
	c, rec := newOpenAIImagesTestContext(t, body)
	svc := newOpenAIImagesTestService(nil)
	account := directImagesTestAccount()
	delete(account.Credentials, "access_token")
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.Nil(t, result)
	var countErr *OpenAIImagesRequestValidationError
	require.ErrorAs(t, err, &countErr, "count validation must precede token retrieval/refresh")
	require.Equal(t, 200, rec.Code)
	require.Empty(t, rec.Body.String(), "handler owns the validation error response")
	require.Equal(t, "n", countErr.Param)
}

func TestResponsesImagesSingleDoesNotEmitN(t *testing.T) {
	parsed := &OpenAIImagesRequest{Endpoint: openAIImagesGenerationsEndpoint, Model: "gpt-image-2.5", Prompt: "draw a circle", N: 1}
	body, err := buildOpenAIImagesResponsesRequest(parsed, parsed.Model)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(body, "tools.0.n").Exists())
	require.Equal(t, "gpt-image-2.5", gjson.GetBytes(body, "tools.0.model").String())
	require.Equal(t, "draw a circle", gjson.GetBytes(body, "input.0.content.0.text").String())
}

func TestNativeAndAPIKeyImagesBatchKeepCount(t *testing.T) {
	for _, tc := range []struct {
		name, requested, mapped string
		apiKey                  bool
	}{
		{"direct", "gpt-image-2", "", false},
		{"bare_mapped_to_direct", "gpt-image-2.5", "gpt-image-2", false},
		{"apikey_bare", "gpt-image-2.5", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"model":%q,"prompt":"draw two circles","n":2}`, tc.requested))
			c, rec := newOpenAIImagesTestContext(t, body)
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"data":[{"b64_json":"AA=="},{"b64_json":"AQ=="}],"usage":{"input_tokens":10,"output_tokens":20}}`))}}
			svc := newOpenAIImagesTestService(upstream)
			account := directImagesTestAccount()
			if tc.apiKey {
				account = newOpenAIImagesAPIKeyAccount()
			}
			if tc.mapped != "" {
				account.Credentials["model_mapping"] = map[string]any{tc.requested: tc.mapped}
			}
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
			require.NoError(t, err)
			require.Equal(t, int64(2), gjson.GetBytes(upstream.lastBody, "n").Int())
			require.False(t, gjson.GetBytes(upstream.lastBody, "tools").Exists())
			require.Equal(t, 2, result.ImageCount)
			require.Len(t, gjson.GetBytes(rec.Body.Bytes(), "data").Array(), 2)
		})
	}
}

func TestResponsesImagesBatchEditRejectsBeforeUpstream(t *testing.T) {
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeSetupToken} {
		t.Run(accountType, func(t *testing.T) {
			c, rec := newOpenAIImagesTestContext(t, []byte(`{}`))
			c.Request.URL.Path = openAIImagesEditsEndpoint
			parsed := &OpenAIImagesRequest{Endpoint: openAIImagesEditsEndpoint, Model: "gpt-image-2.5", Prompt: "edit a circle", N: 2, InputImageURLs: []string{"https://example.com/source.png"}}
			account := directImagesTestAccount()
			account.Type = accountType
			delete(account.Credentials, "access_token")
			svc := newOpenAIImagesTestService(nil)
			result, err := svc.ForwardImages(context.Background(), c, account, nil, parsed, "")
			require.Nil(t, result)
			var validationErr *OpenAIImagesRequestValidationError
			require.ErrorAs(t, err, &validationErr)
			require.Equal(t, "n", validationErr.Param)
			require.Empty(t, rec.Body.String())
		})
	}
}

func TestNativeImagesBatchDoesNotFallBackToUnsupportedResponsesCount(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","prompt":"draw","n":2}`)
	c, _ := newOpenAIImagesTestContext(t, body)
	calls := 0
	upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		calls++
		require.Equal(t, "/backend-api/codex/images/generations", req.URL.Path)
		return &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"type":"not_found_error"}}`))}, nil
	}}
	svc := newOpenAIImagesTestService(upstream)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	result, err := svc.ForwardImages(context.Background(), c, directImagesTestAccount(), body, parsed, "")
	require.Nil(t, result)
	var validationErr *OpenAIImagesRequestValidationError
	require.ErrorAs(t, err, &validationErr)
	require.Equal(t, 1, calls, "do not send the batch again to a Responses tool that cannot honor n")
}
