package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/tidwall/gjson"
)

type deepSeekRecoveryAttemptKey struct{}

// DeepSeekRecoveryUsage contains observed token counts only, never response
// text, tool arguments, credentials or an inferred monetary charge. Anthropic
// input_tokens excludes the separately reported cache token fields.
type DeepSeekRecoveryUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	ReasoningTokens          int64 `json:"reasoning_tokens"`
}

// DeepSeekRecoveryOutcome is a safe snapshot for the handler's attempt log.
// UsageUnknown remains true until both input and output counts were observed;
// observed counts are a lower bound when an upstream stream was interrupted.
type DeepSeekRecoveryOutcome struct {
	Protocol     string                `json:"protocol"`
	FinishReason string                `json:"finish_reason,omitempty"`
	Semantic     bool                  `json:"semantic"`
	Answer       bool                  `json:"answer"`
	ToolCall     bool                  `json:"tool_call"`
	Refusal      bool                  `json:"refusal"`
	Terminal     bool                  `json:"terminal"`
	Usage        DeepSeekRecoveryUsage `json:"usage"`
	UsageUnknown bool                  `json:"usage_unknown"`
	CyberPolicy  *CyberPolicyMark      `json:"cyber_policy,omitempty"`
}

type deepSeekRecoveryTimeouts struct{ firstSemantic, stall, attempt time.Duration }

// DeepSeekRecoveryAttempt owns one selected channel's lifetime. Activation is
// explicit: the handler must install the returned context for every attempt.
// It must inspect Err even if an existing converter returns a nil error.
type DeepSeekRecoveryAttempt struct {
	ctx                   context.Context
	parent                context.Context
	cancel                context.CancelCauseFunc
	mu                    sync.Mutex
	err                   error
	closed                bool
	sent                  map[int64]bool
	out                   DeepSeekRecoveryOutcome
	inputSeen, outputSeen bool
	started, progress     time.Time
	limits                deepSeekRecoveryTimeouts
	wake                  chan struct{}
	done                  chan struct{}
	closeBody             func()
	cleanup               func()
}

func NewDeepSeekRecoveryAttempt(parent context.Context) (context.Context, *DeepSeekRecoveryAttempt) {
	return newDeepSeekRecoveryAttempt(parent, deepSeekRecoveryTimeouts{120 * time.Second, 120 * time.Second, 300 * time.Second})
}

// Separate timeout injection keeps tests short without adding a configuration
// surface or allowing ping frames to extend the total attempt deadline.
func newDeepSeekRecoveryAttempt(parent context.Context, limits deepSeekRecoveryTimeouts) (context.Context, *DeepSeekRecoveryAttempt) {
	ctx, cancel := context.WithCancelCause(parent)
	a := &DeepSeekRecoveryAttempt{ctx: ctx, parent: parent, cancel: cancel,
		sent: make(map[int64]bool), started: time.Now(), limits: limits,
		wake: make(chan struct{}, 1), done: make(chan struct{}),
		out: DeepSeekRecoveryOutcome{UsageUnknown: true}}
	go a.watch()
	return context.WithValue(ctx, deepSeekRecoveryAttemptKey{}, a), a
}

func (a *DeepSeekRecoveryAttempt) Outcome() DeepSeekRecoveryOutcome {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := a.out
	if out.CyberPolicy != nil {
		mark := *out.CyberPolicy
		out.CyberPolicy = &mark
	}
	return out
}

func (a *DeepSeekRecoveryAttempt) ObservedUsage() (DeepSeekRecoveryUsage, bool) {
	o := a.Outcome()
	return o.Usage, o.UsageUnknown
}

func (a *DeepSeekRecoveryAttempt) Err() error {
	a.mu.Lock()
	err := a.err
	a.mu.Unlock()
	if cause := context.Cause(a.parent); cause != nil {
		return cause
	}
	return err
}

// Close cancels the actual HTTP request and closes its body. Successful cleanup
// does not manufacture an error; Err is still useful after Close.
func (a *DeepSeekRecoveryAttempt) Close() {
	a.mu.Lock()
	a.closed = true
	closeBody, cleanup := a.closeBody, a.cleanup
	a.mu.Unlock()
	a.cancel(context.Canceled)
	if closeBody != nil {
		closeBody()
	}
	if cleanup != nil {
		cleanup()
	}
	<-a.done
}

func deepSeekRecoveryFailure(reason string, status int) *UpstreamFailoverError {
	if status == 0 {
		status = http.StatusBadGateway
	}
	return &UpstreamFailoverError{StatusCode: status,
		Stage: GatewayFailureStageInference, Scope: GatewayFailureScopeAccount,
		Reason: GatewayFailureReason("deepseek_" + reason), NextAccountAction: NextAccountRetry,
		RetryableOnSameAccount: false, SameAccountRetryMax: -1,
		ClientStatusCode: http.StatusBadGateway, ClientMessage: "DeepSeek upstream attempt failed"}
}

func (a *DeepSeekRecoveryAttempt) fail(err error) error {
	a.mu.Lock()
	if a.err == nil && !a.closed {
		a.err = err
	}
	stored := a.err
	a.mu.Unlock()
	if stored != nil {
		a.cancel(stored)
	}
	if cause := context.Cause(a.parent); cause != nil {
		return cause
	}
	if stored != nil {
		return stored
	}
	return err
}

func (a *DeepSeekRecoveryAttempt) watch() {
	defer close(a.done)
	timer := time.NewTimer(a.limits.attempt)
	defer timer.Stop()
	for {
		a.mu.Lock()
		deadline, reason := a.started.Add(a.limits.attempt), "attempt_timeout"
		if !a.out.Terminal {
			progressDeadline, progressReason := a.started.Add(a.limits.firstSemantic), "first_semantic_timeout"
			if !a.progress.IsZero() {
				progressDeadline, progressReason = a.progress.Add(a.limits.stall), "stall_timeout"
			}
			if progressDeadline.Before(deadline) {
				deadline, reason = progressDeadline, progressReason
			}
		}
		a.mu.Unlock()
		timer.Reset(time.Until(deadline))
		select {
		case <-a.ctx.Done():
			return
		case <-a.wake:
			continue
		case <-timer.C:
			// Recheck under the state lock: semantic progress may have arrived at
			// the same time as the timer. Never expire on a stale idle deadline.
			a.mu.Lock()
			stale := reason != "attempt_timeout" && (a.out.Terminal || (!a.progress.IsZero() && a.progress.Add(a.limits.stall).After(time.Now())))
			if !stale && a.err == nil && !a.closed {
				a.err = deepSeekRecoveryFailure(reason, http.StatusGatewayTimeout)
			}
			err := a.err
			a.mu.Unlock()
			if stale {
				continue
			}
			if err != nil {
				a.cancel(err)
			}
			return
		}
	}
}

func (a *DeepSeekRecoveryAttempt) notify() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

func deepSeekAttemptForRequest(req *http.Request, account *Account) *DeepSeekRecoveryAttempt {
	if req == nil || account == nil || account.Platform != "deepseek" || req.Method != http.MethodPost || req.URL == nil {
		return nil
	}
	if deepSeekSourceProtocol(req.URL.Path) == "" {
		return nil
	}
	a, _ := req.Context().Value(deepSeekRecoveryAttemptKey{}).(*DeepSeekRecoveryAttempt)
	return a
}

func deepSeekSourceProtocol(path string) string {
	switch path = strings.TrimRight(path, "/"); {
	case strings.HasSuffix(path, "/chat/completions"):
		return "chat_completions"
	case strings.HasSuffix(path, "/messages"):
		return "anthropic"
	case strings.HasSuffix(path, "/responses"):
		return "responses"
	default:
		return ""
	}
}

// Called at doOpenAIUpstream's boundary, after any adapter may have detached
// its context. Values come from the real transport context, cancellation from
// BOTH lifetimes. The admission owner also survives builders using WithoutCancel.
func (s *OpenAIGatewayService) doDeepSeekRecoveryUpstream(req *http.Request, proxyURL string, account *Account, a *DeepSeekRecoveryAttempt) (*http.Response, error) {
	if err := a.Err(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	blocked := a.closed || a.sent[account.ID] || len(a.sent) != 0
	if !blocked {
		a.sent[account.ID] = true
		a.out.Protocol = deepSeekSourceProtocol(req.URL.Path)
	}
	a.mu.Unlock()
	if blocked {
		return nil, a.fail(deepSeekRecoveryFailure("duplicate_submission", 0))
	}

	ctx, cancel := context.WithCancelCause(req.Context())
	stopAttempt := context.AfterFunc(a.ctx, func() { cancel(context.Cause(a.ctx)) })
	stopOwner := func() bool { return true }
	if owner, ok := apiKeyAdmissionOwnerFromContext(req.Context()); ok {
		stopOwner = context.AfterFunc(owner.ctx, func() { cancel(context.Cause(owner.ctx)) })
		if cause := context.Cause(owner.ctx); cause != nil {
			cancel(cause)
		}
	}
	if cause := context.Cause(a.ctx); cause != nil {
		cancel(cause)
	}
	cleanup := sync.OnceFunc(func() { stopAttempt(); stopOwner(); cancel(context.Canceled) })
	a.mu.Lock()
	a.cleanup = cleanup
	closed := a.closed
	a.mu.Unlock()
	if closed {
		cleanup()
		return nil, context.Canceled
	}
	if cause := context.Cause(ctx); cause != nil {
		cleanup()
		return nil, a.fail(cause)
	}
	req = req.Clone(WithHTTPUpstreamRedirectsDisabled(ctx))
	req, safetyInfo := s.applySafetyPromptToRequest(req, account)
	// Repository TLS fallback and net/http's replay paths require GetBody.
	// Disable that capability only on this private outbound clone; the original
	// adapter request stays readable and any retry reenters the attempt guard.
	req.GetBody = nil
	// This single physical send deliberately bypasses runtime proxy fallback.
	// Adapter field repair/retries reenter doOpenAIUpstream and are rejected above.
	resp, err := s.doOpenAIProxyAttempt(req, account, runtimeProxyEgress{url: proxyURL, proxyID: -1})
	if err != nil || resp == nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		cause := context.Cause(ctx)
		cleanup()
		if cause != nil {
			return nil, a.fail(cause)
		}
		return nil, a.fail(deepSeekRecoveryFailure("transport_error", 0))
	}
	if resp.Request == nil {
		resp.Request = req
	}
	if resp.Body == nil {
		cleanup()
		return nil, a.fail(deepSeekRecoveryFailure("missing_body", 0))
	}
	// Capture the original body; resp.Body is replaced below.
	raw := resp.Body
	closeBody := sync.OnceFunc(func() { _ = raw.Close() })
	stopBody := context.AfterFunc(ctx, func() {
		a.fail(context.Cause(ctx))
		closeBody()
	})
	a.mu.Lock()
	a.closeBody = closeBody
	a.cleanup = sync.OnceFunc(func() { stopBody(); cleanup() })
	closed = a.closed
	a.mu.Unlock()
	if closed {
		closeBody()
		stopBody()
		cleanup()
		return nil, context.Canceled
	}
	if cause := context.Cause(ctx); cause != nil {
		closeBody()
		stopBody()
		cleanup()
		return nil, a.fail(cause)
	}
	if resp.StatusCode >= 400 {
		failure := deepSeekRecoveryFailure("http_error", resp.StatusCode)
		// Audit a small JSON error without waiting for EOF or an unbounded slow
		// body. Timeout cancels the real HTTP request, so even a blocked Read
		// stops. The original HTTP failure and retry-next action are preserved.
		timer := time.AfterFunc(deepSeekErrorBodyTimeout, func() {
			cancel(failure)
			closeBody()
		})
		var payload json.RawMessage
		decodeErr := json.NewDecoder(io.LimitReader(raw, deepSeekMaxErrorBody)).Decode(&payload)
		timer.Stop()
		if decodeErr == nil {
			a.observeUsage(gjson.ParseBytes(payload))
			a.observeCyberPolicy(payload, resp.StatusCode)
		}
		closeBody()
		stopBody()
		cleanup()
		return nil, a.fail(failure)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		closeBody()
		stopBody()
		cleanup()
		return nil, a.fail(deepSeekRecoveryFailure("invalid_status", resp.StatusCode))
	}
	body := &deepSeekValidatedBody{source: raw, reader: bufio.NewReader(raw), attempt: a, transport: ctx,
		closeSource: closeBody, release: sync.OnceFunc(func() { stopBody(); cleanup() }),
		protocol: a.Outcome().Protocol, status: resp.StatusCode, tools: make(map[string]*deepSeekSourceTool)}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0]))
	switch {
	case contentType == "text/event-stream":
		body.stream = true
	case contentType == "application/json" || strings.HasSuffix(contentType, "+json"):
	case contentType == "":
		body.sniff = true
	default:
		closeBody()
		stopBody()
		cleanup()
		return nil, a.fail(deepSeekRecoveryFailure("invalid_content_type", 0))
	}
	resp.Body = body
	return s.wrapSafetyPromptResponse(resp, safetyInfo), nil
}

const deepSeekMaxSourceFrame = 4 << 20
const deepSeekMaxSourceJSON = 32 << 20
const deepSeekMaxErrorBody = 64 << 10
const deepSeekErrorBodyTimeout = 500 * time.Millisecond
const deepSeekCyberPolicyMessage = "Upstream returned cyber_policy (details redacted)"

type deepSeekSourceTool struct {
	name               string
	arguments          string
	hasArguments       bool
	custom             bool
	streamingArguments bool
}

// Only one complete SSE event is buffered, not the answer. Error/failed frames
// are withheld before any converter sees them; preceding valid frames continue
// to flow immediately. Nonstream JSON must be validated as a complete document.
type deepSeekValidatedBody struct {
	source               io.ReadCloser
	reader               *bufio.Reader
	attempt              *DeepSeekRecoveryAttempt
	transport            context.Context
	closeSource, release func()
	protocol             string
	status               int
	stream, sniff, ended bool
	pending              []byte
	finish               string
	tools                map[string]*deepSeekSourceTool
}

func (b *deepSeekValidatedBody) Close() error {
	b.closeSource()
	b.release()
	if !b.attempt.Outcome().Terminal && b.attempt.Err() == nil {
		b.attempt.fail(deepSeekRecoveryFailure("missing_terminal", 0))
	}
	return nil
}

func (b *deepSeekValidatedBody) fail(reason string) error {
	b.closeSource()
	return b.attempt.fail(deepSeekRecoveryFailure(reason, 0))
}

func (b *deepSeekValidatedBody) readError(err error) error {
	if failure := b.attempt.Err(); failure != nil {
		return failure
	}
	if cause := context.Cause(b.transport); cause != nil {
		return b.attempt.fail(cause)
	}
	if errors.Is(err, io.EOF) {
		if b.attempt.Outcome().Terminal {
			return io.EOF
		}
		return b.fail("missing_terminal")
	}
	return b.fail("stream_read_error")
}

func (b *deepSeekValidatedBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if err := b.attempt.Err(); err != nil {
		return 0, err
	}
	if cause := context.Cause(b.transport); cause != nil {
		return 0, b.attempt.fail(cause)
	}
	for len(b.pending) == 0 {
		if b.ended {
			return 0, io.EOF
		}
		if b.sniff {
			var first byte
			for {
				var err error
				first, err = b.reader.ReadByte()
				if err != nil {
					return 0, b.readError(err)
				}
				if first != ' ' && first != '\t' && first != '\r' && first != '\n' {
					break
				}
			}
			_ = b.reader.UnreadByte()
			b.stream = first != '{' && first != '['
			b.sniff = false
		}
		var err error
		if b.stream {
			b.pending, err = b.readFrame()
		} else {
			b.pending, err = b.readJSON()
		}
		if err != nil {
			return 0, err
		}
	}
	if err := b.attempt.Err(); err != nil {
		return 0, err
	}
	n := copy(p, b.pending)
	b.pending = b.pending[n:]
	return n, nil
}

func (b *deepSeekValidatedBody) readJSON() ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(b.reader, deepSeekMaxSourceJSON+1))
	if err != nil {
		return nil, b.readError(err)
	}
	if len(raw) > deepSeekMaxSourceJSON {
		return nil, b.fail("response_too_large")
	}
	if err := b.validate(raw, "", false); err != nil {
		return nil, err
	}
	if !b.attempt.Outcome().Terminal {
		return nil, b.fail("missing_terminal")
	}
	b.ended = true
	return raw, nil
}

func (b *deepSeekValidatedBody) readFrame() ([]byte, error) {
	var frame, line, data []byte
	var event string
	for {
		part, err := b.reader.ReadSlice('\n')
		if len(frame)+len(line)+len(part) > deepSeekMaxSourceFrame {
			return nil, b.fail("frame_too_large")
		}
		line = append(line, part...)
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			if len(frame)+len(line) != 0 {
				return nil, b.fail("truncated_frame")
			}
			return nil, b.readError(err)
		}
		frame = append(frame, line...)
		trimmed := bytes.TrimSuffix(bytes.TrimSuffix(line, []byte{'\n'}), []byte{'\r'})
		if len(trimmed) == 0 {
			if event == "error" || event == "response.failed" || event == "response.cancelled" || event == "response.aborted" {
				if json.Valid(bytes.TrimSpace(data)) {
					b.attempt.observeUsage(gjson.ParseBytes(data))
					b.attempt.observeCyberPolicy(data, b.status)
				}
				return nil, b.fail("error_event")
			}
			if len(data) == 0 {
				return frame, nil
			}
			payload := bytes.TrimSpace(data)
			if bytes.Equal(payload, []byte("[DONE]")) {
				if !b.attempt.Outcome().Terminal {
					return nil, b.fail("missing_terminal")
				}
				b.ended = true
				return frame, nil
			}
			if err := b.validate(payload, event, true); err != nil {
				return nil, err
			}
			return frame, nil
		}
		if trimmed[0] != ':' {
			field, value, _ := bytes.Cut(trimmed, []byte{':'})
			value = bytes.TrimPrefix(value, []byte{' '})
			switch string(field) {
			case "data":
				data = append(data, value...)
				data = append(data, '\n')
			case "event":
				event = string(value)
			case "id", "retry":
			default:
				return nil, b.fail("invalid_sse")
			}
		}
		line = nil
	}
}

func (a *DeepSeekRecoveryAttempt) observeUsage(root gjson.Result) {
	u := root.Get("usage")
	if !u.IsObject() {
		u = root.Get("message.usage")
	}
	if !u.IsObject() {
		u = root.Get("response.usage")
	}
	if !u.IsObject() {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	read := func(dst *int64, paths ...string) bool {
		for _, path := range paths {
			v := u.Get(path)
			if v.Type == gjson.Number && v.Float() >= 0 && v.Float() <= 9007199254740991 && v.Float() == float64(v.Int()) {
				if v.Int() > *dst {
					*dst = v.Int()
				}
				return true
			}
		}
		return false
	}
	a.inputSeen = read(&a.out.Usage.InputTokens, "prompt_tokens", "input_tokens") || a.inputSeen
	a.outputSeen = read(&a.out.Usage.OutputTokens, "completion_tokens", "output_tokens") || a.outputSeen
	read(&a.out.Usage.CacheReadInputTokens, "prompt_cache_hit_tokens", "prompt_tokens_details.cached_tokens", "input_tokens_details.cached_tokens", "cache_read_input_tokens")
	read(&a.out.Usage.CacheCreationInputTokens, "cache_creation_input_tokens")
	read(&a.out.Usage.ReasoningTokens, "completion_tokens_details.reasoning_tokens", "output_tokens_details.reasoning_tokens")
	a.out.UsageUnknown = !a.inputSeen || !a.outputSeen
}

// Only exact codes recognized by the existing helper create a mark. Detection
// carries no policy action, raw provider message/body, notification or billing;
// the handler owns those existing contracts after the attempt has finished.
func (a *DeepSeekRecoveryAttempt) observeCyberPolicy(payload []byte, status int) {
	hit, code, _ := detectOpenAICyberPolicy(payload)
	if !hit {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.out.CyberPolicy == nil {
		a.out.CyberPolicy = &CyberPolicyMark{
			Code: code, Message: deepSeekCyberPolicyMessage, UpstreamStatus: status,
			UpstreamInTok: int(a.out.Usage.InputTokens), UpstreamOutTok: int(a.out.Usage.OutputTokens),
		}
	}
}

func (b *deepSeekValidatedBody) progress(answer, refusal bool) {
	a := b.attempt
	a.mu.Lock()
	a.out.Semantic = true
	a.out.Answer = a.out.Answer || answer
	a.out.Refusal = a.out.Refusal || refusal
	a.progress = time.Now()
	a.mu.Unlock()
	a.notify()
}

func nonempty(v gjson.Result) bool {
	return strings.TrimSpace(v.String()) != "" && v.Type != gjson.Null
}

func (b *deepSeekValidatedBody) textContent(v gjson.Result) {
	if v.Type == gjson.String {
		if nonempty(v) {
			b.progress(true, false)
		}
		return
	}
	for _, block := range v.Array() {
		switch block.Get("type").String() {
		case "text", "output_text":
			if nonempty(block.Get("text")) {
				b.progress(true, false)
			}
		case "refusal":
			b.progress(false, true)
		case "thinking", "reasoning", "redacted_thinking":
			if nonempty(block.Get("thinking")) || nonempty(block.Get("text")) || nonempty(block.Get("data")) {
				b.progress(false, false)
			}
		}
	}
}

func (b *deepSeekValidatedBody) tool(key string, name, args gjson.Result, replace, custom bool) error {
	t := b.tools[key]
	if t == nil {
		t = &deepSeekSourceTool{}
		b.tools[key] = t
	}
	if nonempty(name) {
		t.name = name.String()
		b.progress(false, false)
	}
	t.custom = custom
	if args.Exists() && args.Type != gjson.Null {
		value := args.String()
		if args.IsObject() {
			value = args.Raw
		}
		if replace {
			t.arguments = value
		} else {
			t.arguments += value
		}
		t.hasArguments = true
		if len(t.arguments) > deepSeekMaxSourceFrame {
			return b.fail("tool_arguments_too_large")
		}
		if value != "" {
			b.progress(false, false)
		}
	}
	return nil
}

func (b *deepSeekValidatedBody) terminal(reason string, final bool) error {
	switch reason {
	case "insufficient_system_resource", "aborted", "error", "failed", "cancelled":
		return b.fail("failure_finish")
	case "stop", "end_turn", "stop_sequence", "completed", "length", "max_tokens", "max_output_tokens", "tool_calls", "function_call", "tool_use", "pause_turn", "content_filter", "refusal":
	default:
		return b.fail("invalid_finish")
	}
	if reason == "content_filter" || reason == "refusal" {
		b.progress(false, true)
	}
	validTool := false
	for _, tool := range b.tools {
		if tool.name == "" || !tool.hasArguments || (!tool.custom && !json.Valid([]byte(tool.arguments))) {
			return b.fail("incomplete_tool_call")
		}
		validTool = true
	}
	o := b.attempt.Outcome()
	if !o.Answer && !o.Refusal && !validTool {
		return b.fail("no_answer")
	}
	b.finish = reason
	b.attempt.mu.Lock()
	b.attempt.out.FinishReason = reason
	b.attempt.out.ToolCall = validTool
	b.attempt.out.Terminal = final
	b.attempt.mu.Unlock()
	b.attempt.notify()
	return nil
}

func (b *deepSeekValidatedBody) validate(raw []byte, event string, stream bool) error {
	if !json.Valid(raw) {
		return b.fail("invalid_json")
	}
	r := gjson.ParseBytes(raw)
	if !r.IsObject() {
		return b.fail("invalid_envelope")
	}
	b.attempt.observeUsage(r)
	typ := r.Get("type").String()
	if event == "error" || event == "response.failed" || typ == "error" || typ == "response.failed" ||
		(r.Get("error").Exists() && r.Get("error").Type != gjson.Null) ||
		(r.Get("response.error").Exists() && r.Get("response.error").Type != gjson.Null) {
		b.attempt.observeCyberPolicy(raw, b.status)
		return b.fail("error_envelope")
	}
	switch b.protocol {
	case "chat_completions":
		return b.validateCC(r, stream)
	case "anthropic":
		return b.validateAnthropic(r, event, stream)
	case "responses":
		return b.validateResponses(r, event, stream)
	default:
		return b.fail("invalid_protocol")
	}
}

func (b *deepSeekValidatedBody) validateCC(r gjson.Result, stream bool) error {
	choices := r.Get("choices")
	if !choices.IsArray() {
		return b.fail("invalid_envelope")
	}
	for _, choice := range choices.Array() {
		msg := choice.Get("message")
		if stream {
			msg = choice.Get("delta")
		}
		b.textContent(msg.Get("content"))
		if nonempty(msg.Get("refusal")) {
			b.progress(false, true)
		}
		if nonempty(msg.Get("reasoning_content")) || nonempty(msg.Get("reasoning")) {
			b.progress(false, false)
		}
		for i, t := range msg.Get("tool_calls").Array() {
			index := fmt.Sprint(i)
			if t.Get("index").Exists() {
				index = t.Get("index").String()
			}
			if err := b.tool(choice.Get("index").String()+":"+index, t.Get("function.name"), t.Get("function.arguments"), !stream, false); err != nil {
				return err
			}
		}
		if t := msg.Get("function_call"); t.IsObject() {
			if err := b.tool("legacy:"+choice.Get("index").String(), t.Get("name"), t.Get("arguments"), !stream, false); err != nil {
				return err
			}
		}
		if reason := choice.Get("finish_reason"); nonempty(reason) {
			if err := b.terminal(reason.String(), true); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *deepSeekValidatedBody) anthropicBlock(key string, block gjson.Result) error {
	switch block.Get("type").String() {
	case "text":
		if nonempty(block.Get("text")) {
			b.progress(true, false)
		}
	case "refusal":
		b.progress(false, true)
	case "thinking", "redacted_thinking":
		if nonempty(block.Get("thinking")) || nonempty(block.Get("data")) {
			b.progress(false, false)
		}
	case "tool_use", "server_tool_use":
		return b.tool(key, block.Get("name"), block.Get("input"), true, false)
	}
	return nil
}

func (b *deepSeekValidatedBody) validateAnthropic(r gjson.Result, event string, stream bool) error {
	typ := r.Get("type").String()
	if typ == "" {
		typ = event
	}
	if !stream {
		if typ != "message" || !r.Get("content").IsArray() {
			return b.fail("invalid_envelope")
		}
		for i, block := range r.Get("content").Array() {
			if err := b.anthropicBlock(fmt.Sprint(i), block); err != nil {
				return err
			}
		}
		return b.terminal(r.Get("stop_reason").String(), true)
	}
	key := r.Get("index").String()
	switch typ {
	case "message_start", "ping", "content_block_stop":
		return nil
	case "content_block_start":
		return b.anthropicBlock(key, r.Get("content_block"))
	case "content_block_delta":
		delta := r.Get("delta")
		switch delta.Get("type").String() {
		case "text_delta":
			if nonempty(delta.Get("text")) {
				b.progress(true, false)
			}
		case "thinking_delta", "signature_delta":
			if nonempty(delta.Get("thinking")) || nonempty(delta.Get("signature")) {
				b.progress(false, false)
			}
		case "input_json_delta":
			// A streamed tool_use starts with input:{}, followed by its real JSON.
			if t := b.tools[key]; t != nil && !t.streamingArguments {
				t.arguments, t.streamingArguments = "", true
			}
			return b.tool(key, gjson.Result{}, delta.Get("partial_json"), false, false)
		default:
			return b.fail("invalid_event")
		}
	case "message_delta":
		if reason := r.Get("delta.stop_reason"); nonempty(reason) {
			return b.terminal(reason.String(), false)
		}
	case "message_stop":
		if b.finish == "" {
			return b.fail("missing_terminal")
		}
		return b.terminal(b.finish, true)
	default:
		return b.fail("invalid_event")
	}
	return nil
}

func (b *deepSeekValidatedBody) responseItem(key string, item gjson.Result, replace bool) error {
	switch item.Get("type").String() {
	case "message":
		b.textContent(item.Get("content"))
	case "reasoning":
		if len(item.Get("summary").Array()) > 0 || nonempty(item.Get("encrypted_content")) {
			b.progress(false, false)
		}
	case "function_call":
		return b.tool(key, item.Get("name"), item.Get("arguments"), replace, false)
	case "custom_tool_call":
		return b.tool(key, item.Get("name"), item.Get("input"), replace, true)
	}
	return nil
}

func (b *deepSeekValidatedBody) responseTerminal(r gjson.Result, typ string) error {
	status := r.Get("status").String()
	if status == "failed" || status == "cancelled" || status == "aborted" {
		return b.fail("failure_finish")
	}
	if err := r.Get("error"); err.Exists() && err.Type != gjson.Null {
		return b.fail("error_envelope")
	}
	for i, item := range r.Get("output").Array() {
		if err := b.responseItem(fmt.Sprint(i), item, true); err != nil {
			return err
		}
	}
	if status == "incomplete" || typ == "response.incomplete" {
		reason := r.Get("incomplete_details.reason").String()
		if reason != "max_output_tokens" && reason != "content_filter" {
			return b.fail("failure_finish")
		}
		return b.terminal(reason, true)
	}
	if status != "completed" && typ != "response.completed" {
		return b.fail("missing_terminal")
	}
	return b.terminal("completed", true)
}

func (b *deepSeekValidatedBody) validateResponses(r gjson.Result, event string, stream bool) error {
	if !stream {
		return b.responseTerminal(r, "")
	}
	typ := r.Get("type").String()
	if typ == "" {
		typ = event
	}
	key := r.Get("output_index").String()
	switch typ {
	case "response.completed", "response.incomplete":
		return b.responseTerminal(r.Get("response"), typ)
	case "response.failed", "response.cancelled", "response.aborted":
		return b.fail("failure_finish")
	case "response.output_text.delta":
		if nonempty(r.Get("delta")) {
			b.progress(true, false)
		}
	case "response.output_text.done":
		if nonempty(r.Get("text")) {
			b.progress(true, false)
		}
	case "response.refusal.delta", "response.refusal.done":
		b.progress(false, true)
	case "response.reasoning_text.delta", "response.reasoning_summary_text.delta":
		if nonempty(r.Get("delta")) {
			b.progress(false, false)
		}
	case "response.output_item.added", "response.output_item.done":
		return b.responseItem(key, r.Get("item"), true)
	case "response.function_call_arguments.delta":
		return b.tool(key, gjson.Result{}, r.Get("delta"), false, false)
	case "response.function_call_arguments.done":
		return b.tool(key, r.Get("name"), r.Get("arguments"), true, false)
	case "response.custom_tool_call_input.delta":
		return b.tool(key, gjson.Result{}, r.Get("delta"), false, true)
	case "response.custom_tool_call_input.done":
		return b.tool(key, r.Get("name"), r.Get("input"), true, true)
	default:
		if !strings.HasPrefix(typ, "response.") {
			return b.fail("invalid_event")
		}
	}
	return nil
}
