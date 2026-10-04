package service

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/tidwall/gjson"
)

const GatewayBorrowCandyPrompt = `在一个黑色的袋子里放有三种口味的糖果，每种糖果有两种不同的形状（圆形和五角星形，不同的形状靠手感可以分辨）。现已知不同口味的糖和不同形状的数量统计如下表。参赛者需要在活动前决定摸出的糖果数目，那么，最少取出多少个糖果才能保证手中同时拥有不同形状的苹果味和桃子味的糖？（同时手中有圆形苹果味匹配五角星桃子味糖果，或者有圆形桃子味匹配五角星苹果味糖果都满足要求）
苹果味 桃子味 西瓜味
圆形 7 9 8
五角星形 7 6 4

只输出最终整数，不要解释。`

func gatewayBorrowProbeText(raw []byte) string {
	var deltas strings.Builder
	var completed string
	for _, payload := range openAICodexStateStreamEvents(raw) {
		event := gjson.ParseBytes(payload)
		if event.Get("type").String() == "response.output_text.delta" {
			_, _ = deltas.WriteString(event.Get("delta").String())
		}
		if event.Get("type").String() == "response.completed" {
			var text strings.Builder
			for _, item := range event.Get("response.output").Array() {
				for _, part := range item.Get("content").Array() {
					if part.Get("type").String() == "output_text" {
						_, _ = text.WriteString(part.Get("text").String())
					}
				}
			}
			completed = text.String()
		}
	}
	if completed != "" {
		return strings.TrimSpace(completed)
	}
	return strings.TrimSpace(deltas.String())
}

// Revalidate quality on the exact borrowed route. No account-state writer,
// gateway recursion, usage persistence or user request replay participates.
func ProbeOpenAICodexBorrowQualityRoute(ctx context.Context, upstream HTTPUpstream, template *http.Request, proxy string, accountID int64, concurrency int, profile *tlsfingerprint.Profile, model string) *OpenAICodexStateProbeResult {
	result := ProbeOpenAICodexStateRouteForModel(ctx, upstream, template, proxy, accountID, concurrency, profile, model)
	if result.Verdict != OpenAICodexStateHealthy {
		return result
	}
	pinned, err := template.Cookie("__oailb")
	if err != nil || pinned.Value == "" {
		result.fail("route_changed", "target_route_changed", "")
		return result
	}
	changed := false
	shot, err := fireOpenAICodexProbeShotRequest(ctx, template.Header, result.Model, "", pinned.String(), GatewayBorrowCandyPrompt, func(req *http.Request) (*http.Response, error) {
		req = req.WithContext(WithHTTPUpstreamRedirectsDisabled(req.Context()))
		response, err := upstream.DoWithTLS(req, proxy, accountID, concurrency, profile)
		if response != nil {
			for _, cookie := range response.Cookies() {
				if cookie.Name == "__oailb" && (cookie.Value != pinned.Value || cookie.MaxAge < 0 || (!cookie.Expires.IsZero() && !time.Now().Before(cookie.Expires))) {
					changed = true
				}
			}
		}
		return response, err
	})
	if changed {
		result.fail("route_changed", "target_route_changed", "")
		return result
	}
	if !result.shotUsable(ctx, "borrow quality", shot, err) {
		return result
	}
	if shot.completedModel != result.Model || shot.text != "21" {
		result.fail("quality_failed", "target_quality_failed", "")
		return result
	}
	// Require an exact completed model, not merely a created model followed by a
	// different terminal. The shot's common validator enforces stream completion.
	result.Reason = "target_probe_passed"
	return result
}
