package repository

import (
	"fmt"
	"strconv"
	"strings"
)

// This catalogue is independent of account, channel, group and mutable retail
// pricing. Values are USD per million tokens, reviewed against official model
// cards on 2026-10-03. Historical and new rows use current list-price equivalence;
// this is not an upstream invoice or OAuth subscription spend.
// https://developers.openai.com/api/docs/models/gpt-6-astra
// https://developers.openai.com/api/docs/models/gpt-6.1-sol
// https://developers.openai.com/api/docs/models/gpt-6-luna
// https://developers.openai.com/api/docs/models/gpt-5.6-sol
// https://developers.openai.com/api/docs/models/gpt-5.6-terra
// https://developers.openai.com/api/docs/models/gpt-5.6-luna
// https://developers.openai.com/api/docs/models/gpt-5.5
// https://developers.openai.com/api/docs/models/gpt-5.4
// https://platform.claude.com/docs/en/about-claude/pricing
// https://docs.x.ai/developers/pricing
// https://api-docs.deepseek.com/quick_start/pricing/
// DeepSeek entries are off-peak rates; request UTC time determines a 2x peak.
// Unknown model, tier and media prices remain unpriced, never retail fallbacks.
type apiEquivalentPrice struct {
	model                                                         string
	input, cachedInput, output, cacheWrite5m, cacheWrite1h        float64
	contextThreshold                                              int
	contextInclusive                                              bool
	contextInput, contextOutput, priority, flex, batch, ultrafast float64
	peakUTC                                                       bool
}

func openAIEquivalentPrice(model string, input, cachedInput, output, priority, ultrafast float64) apiEquivalentPrice {
	return apiEquivalentPrice{model, input, cachedInput, output, input * 1.25, input * 1.25,
		272000, false, 2, 1.5, priority, .5, .5, ultrafast, false}
}

func claudeEquivalentPrice(model string, input, cachedInput, output, cache5m, cache1h float64, threshold int) apiEquivalentPrice {
	return apiEquivalentPrice{model, input, cachedInput, output, cache5m, cache1h,
		threshold, false, 2, 1.5, 0, 0, .5, 0, false}
}

func grokEquivalentPrice(model string, input, cachedInput, output float64) apiEquivalentPrice {
	return apiEquivalentPrice{model, input, cachedInput, output, 0, 0,
		200000, true, 2, 2, 0, 0, .5, 0, false}
}

func deepseekEquivalentPrice(model string, input, cachedInput, output float64) apiEquivalentPrice {
	return apiEquivalentPrice{model, input, cachedInput, output, 0, 0,
		0, false, 1, 1, 0, 0, 0, 0, true}
}

var accountAPIEquivalentPrices = []apiEquivalentPrice{
	openAIEquivalentPrice("gpt-6-astra", 10, 1, 50, 2, 6),
	openAIEquivalentPrice("gpt-6.1-sol", 2, .1, 10, 2, 0),
	openAIEquivalentPrice("gpt-6-sol", 2, .2, 10, 2, 0),
	openAIEquivalentPrice("gpt-6-luna", .1, .01, .5, 2, 0),
	openAIEquivalentPrice("gpt-5.6-sol", 4, .4, 20, 2, 0),
	openAIEquivalentPrice("gpt-5.6-terra", 2, .2, 12, 2, 0),
	openAIEquivalentPrice("gpt-5.6-luna", .2, .02, 1.2, 2, 0),
	openAIEquivalentPrice("gpt-5.5", 5, .5, 30, 0, 0),
	openAIEquivalentPrice("gpt-5.4", 2.5, .25, 15, 2, 0),
	claudeEquivalentPrice("claude-fable-5-1", 10, .25, 50, 12.5, 20, 0),
	claudeEquivalentPrice("claude-opus-5-5", 4, .2, 20, 5, 8, 0),
	claudeEquivalentPrice("claude-fable-5", 10, 1, 50, 12.5, 20, 0),
	claudeEquivalentPrice("claude-opus-5", 5, .5, 25, 6.25, 10, 0),
	claudeEquivalentPrice("claude-opus-4-8", 5, .5, 25, 6.25, 10, 0),
	claudeEquivalentPrice("claude-opus-4-7", 5, .5, 25, 6.25, 10, 0),
	claudeEquivalentPrice("claude-opus-4-6", 5, .5, 25, 6.25, 10, 0),
	claudeEquivalentPrice("claude-opus-4-5", 5, .5, 25, 6.25, 10, 0),
	claudeEquivalentPrice("claude-sonnet-5", 2, .2, 10, 2.5, 4, 0),
	claudeEquivalentPrice("claude-sonnet-5-5", 2, .2, 10, 2.5, 4, 0),
	claudeEquivalentPrice("claude-sonnet-4-6", 3, .3, 15, 3.75, 6, 0),
	claudeEquivalentPrice("claude-sonnet-4-5", 3, .3, 15, 3.75, 6, 200000),
	claudeEquivalentPrice("claude-haiku-4-5", 1, .1, 5, 1.25, 2, 0),
	grokEquivalentPrice("grok-4.5", 2, .30, 6),
	grokEquivalentPrice("grok-4.6", 2, .50, 6),
	grokEquivalentPrice("grok-4.7", 2, .50, 6),
	grokEquivalentPrice("grok-4.20-reasoning", 1.25, .20, 2.5),
	grokEquivalentPrice("grok-4.20-non-reasoning", 1.25, .20, 2.5),
	deepseekEquivalentPrice("deepseek-flash", .15, .003, .6),
	deepseekEquivalentPrice("deepseek-v4.1-flash", .15, .003, .6),
	deepseekEquivalentPrice("deepseek-v4-pro", .66, .022, 1.98),
	deepseekEquivalentPrice("deepseek-v4-pro-0813", .66, .022, 1.98),
}

func accountAPIEquivalentRowsCTE(where string) string {
	values := make([]string, 0, len(accountAPIEquivalentPrices))
	for _, p := range accountAPIEquivalentPrices {
		// All values are code-owned; no operator or request strings enter the SQL.
		values = append(values, fmt.Sprintf("('%s', %s, %s, %s, %s, %s, %d, %t, %s, %s, %s, %s, %s, %s, %t)",
			p.model, apiEquivalentNumber(p.input), apiEquivalentNumber(p.cachedInput),
			apiEquivalentNumber(p.output), apiEquivalentNumber(p.cacheWrite5m), apiEquivalentNumber(p.cacheWrite1h),
			p.contextThreshold, p.contextInclusive, apiEquivalentNumber(p.contextInput),
			apiEquivalentNumber(p.contextOutput), apiEquivalentNumber(p.priority), apiEquivalentNumber(p.flex),
			apiEquivalentNumber(p.batch), apiEquivalentNumber(p.ultrafast), p.peakUTC))
	}
	return `WITH api_equivalent_pricing(model, input_price, cache_price, output_price, write5_price, write1_price,
 context_threshold, context_inclusive, context_input, context_output, priority, flex, batch, ultrafast, peak_utc) AS (VALUES ` + strings.Join(values, ",") + `),
 api_equivalent_rows AS (
 SELECT ul.*, CASE
  -- Tokenless station per-request fees are not provider token consumption.
  WHEN input_tokens = 0 AND output_tokens = 0 AND cache_creation_tokens = 0
   AND cache_read_tokens = 0 AND image_input_tokens = 0 AND image_output_tokens = 0
   AND image_count = 0 AND video_count = 0
   AND COALESCE(billing_mode, '') NOT IN ('image', 'video') THEN 0::numeric
  WHEN p.model IS NULL OR image_input_tokens <> 0 OR image_output_tokens <> 0
   OR image_count <> 0 OR video_count <> 0 OR COALESCE(billing_mode, '') IN ('image', 'video')
   OR input_tokens < 0 OR output_tokens < 0 OR cache_creation_tokens < 0 OR cache_read_tokens < 0
   OR cache_creation_5m_tokens < 0 OR cache_creation_1h_tokens < 0
   OR cache_creation_5m_tokens::bigint + cache_creation_1h_tokens > cache_creation_tokens
   OR (cache_creation_tokens > 0 AND p.write5_price = 0) THEN NULL
  ELSE (
   (input_tokens * p.input_price + (cache_creation_tokens - cache_creation_1h_tokens) * p.write5_price
     + cache_creation_1h_tokens * p.write1_price + cache_read_tokens * p.cache_price)
   * CASE WHEN ctx.long_context THEN p.context_input ELSE 1 END
   + output_tokens * p.output_price * CASE WHEN ctx.long_context THEN p.context_output ELSE 1 END
  ) / 1000000 * CASE LOWER(TRIM(COALESCE(service_tier, '')))
   WHEN '' THEN 1 WHEN 'default' THEN 1 WHEN 'auto' THEN 1 WHEN 'standard' THEN 1
   WHEN 'priority' THEN NULLIF(p.priority, 0) WHEN 'fast' THEN NULLIF(p.priority, 0)
   WHEN 'flex' THEN NULLIF(p.flex, 0) WHEN 'batch' THEN NULLIF(p.batch, 0)
   WHEN 'ultrafast' THEN NULLIF(p.ultrafast, 0)
   ELSE NULL END * CASE WHEN p.peak_utc
   AND EXTRACT(ISODOW FROM ul.created_at AT TIME ZONE 'UTC') BETWEEN 1 AND 5
   AND (((ul.created_at AT TIME ZONE 'UTC')::time >= '01:00'::time AND (ul.created_at AT TIME ZONE 'UTC')::time < '04:00'::time)
    OR ((ul.created_at AT TIME ZONE 'UTC')::time >= '06:00'::time AND (ul.created_at AT TIME ZONE 'UTC')::time < '10:00'::time))
   THEN 2 ELSE 1 END
 END AS api_equivalent_row_cost
 FROM usage_logs ul
 CROSS JOIN LATERAL (SELECT REGEXP_REPLACE(REGEXP_REPLACE(
  LOWER(COALESCE(NULLIF(TRIM(ul.upstream_response_model), ''), NULLIF(TRIM(ul.upstream_model), ''), TRIM(ul.model))),
  '-[0-9]{4}-?[0-9]{2}-?[0-9]{2}$', ''),
  '-(none|minimal|low|medium|high|xhigh|max|ultra)$', '') AS name) effective_model
 LEFT JOIN api_equivalent_pricing p ON p.model = CASE WHEN LEFT(effective_model.name, 7) = 'claude-'
  THEN REPLACE(effective_model.name, '.', '-') ELSE effective_model.name END
 CROSS JOIN LATERAL (SELECT p.context_threshold > 0 AND (
   (input_tokens::bigint + cache_creation_tokens + cache_read_tokens) > p.context_threshold
   OR (p.context_inclusive AND (input_tokens::bigint + cache_creation_tokens + cache_read_tokens) = p.context_threshold)
  ) AS long_context) ctx
 WHERE ` + where + `
 ) `
}

func apiEquivalentNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

const accountAPIEquivalentAggregate = `,
 CASE WHEN COUNT(*) = 0 THEN 0::numeric ELSE SUM(api_equivalent_row_cost) END AS api_equivalent_cost,
 COUNT(*) FILTER (WHERE api_equivalent_row_cost IS NULL) AS api_equivalent_unpriced_requests`
