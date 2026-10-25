# DeepSeek tools protocol adapter

This standard-library-only package contains no service, transport, database or
account-selection dependencies. Its public API is:

```go
ParseRequest(operation string, raw []byte) (*Request, error)
(*Request).AlphaBody(model string) ([]byte, error)
AdaptResponse(raw []byte, request *Request) ([]byte, bool, error)
```

`operation` is exactly `search` or `fetch`. `Request` has exported fields
`Operation`, `Query`, `Count`, `NeedContent`, `URL`, `OriginalQuestion`, and
`Localization`. Count defaults to 10. NeedContent defaults to false for search and
true for fetch; explicit false always produces an empty content field. Absent or
null ori_question maps to an empty OriginalQuestion. Query and ori_question limits
count Unicode code points; URLs are limited to 8192 UTF-8 bytes. Known fields are
strictly typed, including optional fields. Unknown fields are ignored.

AlphaBody generates a new UUIDv4 and the neighboring standalone alpha schema:
model, input message, commands.search_query or commands.open,
commands.response_length=long, settings.allowed_callers=[direct] and
settings.external_web_access=true. It never includes store. Count limits results
locally; it cannot make the backend return additional sources. OriginalQuestion is
included as input context. Localization is preserved and validated, but is not
converted from an IP into a fabricated OpenAI geographic user_location.

The AdaptResponse bool means **usable result**, not a truncation flag. A confirmed
empty search returns a non-nil JSON body, false, nil. Invalid/unrecognized payloads,
upstream errors, failed page opens, and fetches without text for the requested URL
return nil, false, error. Input JSON is limited to 2 MiB. Responses have a 128 KiB
aggregate readable-string budget, 64 KiB per page content, 4 KiB per snippet or
summary, 2 KiB per title and a hard 1 MiB serialized output bound. Truncation respects
UTF-8 boundaries. Internal refs and encrypted_output are never copied as fields.

Native source blocks are attributed independently. Search result count is clipped,
never padded. PAT text_result rows are citations: output is a generated combined
answer and is never copied into per-page snippet/content. Fetch requires readable
page text; citation-only PAT responses fail. Dates/rerank scores remain null and
authority is -1. Responses disclose provider=openai and partial=true because alpha
excerpts do not establish complete page retrieval. truncated=true indicates known
upstream clipping, local text limits, count clipping or discarded source blocks.
The adapter does not infer completeness from wordlim, line counts or HTTP success.

URL checks reject local/private/multicast/metadata literals, userinfo and ambiguous
numeric hosts. DNS lookup, redirect policies and DNS rebinding protections belong
to the transport; this package performs no network requests. Fetch matches the
requested page URL (ignoring fragment and bare trailing slash); an unrelated source
is never substituted. No response headers or error-body secrets are propagated.

Normal integration test, with the repository's required Go toolchain:

```sh
go test -mod=readonly ./internal/pkg/deepseektools
```

To test this self-contained package on the installed Go 1.27.0 while the parent
module requires 1.27.3, run inside this directory with GO111MODULE=off. Set GOCACHE
and GOTMPDIR explicitly inside this directory if writes must remain here.
