# DeepSeek-compatible search tools

The gateway can expose `POST /v1/tools/search` and `POST /v1/tools/fetch` to authenticated DeepSeek group keys. The search backend is OpenAI's existing standalone alpha search service; DeepSeek model generation stays on its existing route.

## Operator configuration

Disabled by default. Enable explicitly with:

```env
GATEWAY_DEEPSEEK_TOOLS_ENABLED=true
GATEWAY_DEEPSEEK_TOOLS_SEARCH_GROUP_ID=12
GATEWAY_DEEPSEEK_TOOLS_SEARCH_MODEL=gpt-6-astra
```

The group ID must identify an active OpenAI group on the actual installation; `12` is an example, not a universal ID. Only accounts admitted by that pool's existing scheduler can serve the tools. Caller keys remain assigned to their original DeepSeek group. User authentication, balance, subscription, per-key limits, platform quota and billing group are retained.

## Requests

Send the existing DeepSeek group API key as `Authorization: Bearer ...`.

```json
{"query":"DeepSeek Search Fetch documentation","count":3,"need_content":false}
```

`query` accepts 1–128 nonblank Unicode characters. `count` defaults to 10 and accepts 1–50. `need_content` defaults to false. `ori_question` is optional context. `localization` accepts an IP hint for compatibility, but is not converted into an invented geographic location. Unknown fields are ignored.

Fetch:

```json
{"url":"https://api-docs.deepseek.com/guides/search/"}
```

The URL must be a public HTTP(S) address with no embedded username/password. Private, loopback, metadata and ambiguous numeric literal destinations are rejected before upstream dispatch.

## Responses and limitations

Search returns a `results` array with title, URL, snippet, summary and content. `need_content:false` always returns an empty content string. Canonical `date`, `site_name` and `site_icon` fields are included; unavailable values are null or empty, with `publish_time` retained as an alias. Unknown publication time and score are null; unknown authority is -1. Results are capped by count; fewer sources may be available.

`need_content:true` and Fetch select a native Alpha backend; citation-only PAT fallback remains available for metadata-only search. Fetch returns title, URL and readable page content. Both endpoints add `provider:"openai"`, `partial` and `truncated` metadata. Alpha search supplies bounded excerpts, not a guarantee of complete original-page Markdown, so page content is marked partial. A clipped result is explicitly marked truncated. Model-generated PAT summaries and encrypted tool context are never represented as raw page content. An unreadable/failed page open returns an upstream error instead of a fabricated successful page.

Only a successful adapted response with usable results is charged, using the original calling group's existing `web_search_price_per_call` (default $0.01 per successful tool request), group/user multiplier and quota rules. Empty searches, validation errors, failed upstream calls and failed response conversion are not charged. Fetch is one successful tool request under the same tariff. Usage retains the original user/key/group, identifies `deepseek-search` or `deepseek-fetch`, and records the actual OpenAI backend model/endpoint separately.

These are search-tool compatibility endpoints. They do not automatically enable model-native `web_search` or change a client application's tool configuration.
