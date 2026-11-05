package handler

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	deepSeekRecoveryNotice       = "\n\n[上游中断，正在重新生成]\n\n"
	deepSeekRecoveryEventLimit   = 1 << 20
	deepSeekRecoveryPreludeLimit = 64 << 10
	deepSeekRecoveryBodyLimit    = 16 << 20
	deepSeekRecoveryBlockLimit   = 1024
)

type deepSeekRecoveryFrame struct {
	event string
	data  map[string]any
}
type deepSeekRecoveryBlock struct {
	kind              string
	index             int
	open, exposedOpen bool
}

// deepSeekRecoveryStream stages protocol metadata and terminal events, never a
// streaming answer. The coordinator owns source validation, retries and billing.
// All methods except Header (the usual net/http header-map contract) serialize
// access. Status/Size/Written describe the current virtual attempt; heartbeats
// only affect the real downstream writer.
type deepSeekRecoveryStream struct {
	mu                                          sync.Mutex
	downstream                                  gin.ResponseWriter
	protocol                                    string
	stream                                      bool
	header                                      http.Header
	status, size                                int
	active, ended, finalized                    bool
	err, clientErr                              error
	clientGone, toolExposed, business           bool
	attemptBusiness, attemptTool, pendingNotice bool
	id, model                                   string
	created                                     int64
	body                                        bytes.Buffer
	line, data                                  []byte
	event                                       string
	eventBytes                                  int
	skipLF, comment, hasData                    bool
	prelude, terminal                           []deepSeekRecoveryFrame
	preludeBytes, terminalBytes                 int
	ccFinish, ccDone                            bool
	ccToolFinish                                bool
	messageStart                                map[string]any
	messageUsage                                map[string]any
	messageEmitted, messageDelta, messageStop   bool
	nextIndex                                   int
	blocks                                      map[int]*deepSeekRecoveryBlock
}

var _ gin.ResponseWriter = (*deepSeekRecoveryStream)(nil)

// Only chat (Chat Completions) and messages are supported. Unsupported
// protocols fail closed through EndAttempt; they are never passed through.
func newDeepSeekRecoveryStream(w gin.ResponseWriter, protocol string, stream bool) *deepSeekRecoveryStream {
	return &deepSeekRecoveryStream{downstream: w, protocol: protocol, stream: stream,
		header: make(http.Header), status: http.StatusOK, size: -1,
		id: strings.ReplaceAll(uuid.NewString(), "-", ""), created: time.Now().Unix()}
}

// BeginAttempt also prepares a retry. Its notice is delayed until new business
// output actually arrives. An empty admission attempt may be abandoned.
func (w *deepSeekRecoveryStream) BeginAttempt() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.finalized || w.clientGone || w.toolExposed {
		return errors.New("deepseek recovery: cannot begin another attempt")
	}
	if w.active && !w.ended && w.size > 0 {
		return errors.New("deepseek recovery: previous attempt not ended")
	}
	if err := w.closeNonToolBlocks(); err != nil {
		return err
	}
	w.header = make(http.Header)
	w.status, w.size = http.StatusOK, -1
	w.active, w.ended, w.err = true, false, nil
	w.attemptBusiness, w.attemptTool, w.pendingNotice = false, false, w.business
	w.body = bytes.Buffer{}
	w.line, w.data, w.event = nil, nil, ""
	w.eventBytes, w.skipLF, w.comment, w.hasData = 0, false, false, false
	w.prelude, w.terminal = nil, nil
	w.preludeBytes, w.terminalBytes = 0, 0
	w.ccFinish, w.ccDone, w.ccToolFinish = false, false, false
	w.messageStart, w.messageUsage = nil, nil
	w.messageDelta, w.messageStop = false, false
	w.blocks = make(map[int]*deepSeekRecoveryBlock)
	if w.protocol != "chat" && w.protocol != "messages" {
		w.err = errors.New("deepseek recovery: unsupported protocol")
	}
	return w.err
}

func (w *deepSeekRecoveryStream) Header() http.Header {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.header
}
func (w *deepSeekRecoveryStream) WriteHeader(status int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.size < 0 && status >= 200 && status <= 599 {
		w.status = status
	}
}
func (w *deepSeekRecoveryStream) WriteHeaderNow() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.size < 0 {
		w.size = 0
	}
}
func (w *deepSeekRecoveryStream) Status() int                       { w.mu.Lock(); defer w.mu.Unlock(); return w.status }
func (w *deepSeekRecoveryStream) Size() int                         { w.mu.Lock(); defer w.mu.Unlock(); return w.size }
func (w *deepSeekRecoveryStream) Written() bool                     { return w.Size() >= 0 }
func (w *deepSeekRecoveryStream) WriteString(s string) (int, error) { return w.Write([]byte(s)) }
func (w *deepSeekRecoveryStream) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.size < 0 {
		w.size = 0
	}
	if !w.clientGone && !w.finalized && w.downstream.Written() {
		w.downstream.Flush()
	}
}
func (w *deepSeekRecoveryStream) CloseNotify() <-chan bool { return w.downstream.CloseNotify() }
func (w *deepSeekRecoveryStream) Pusher() http.Pusher      { return nil }
func (w *deepSeekRecoveryStream) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("deepseek recovery: hijack unsupported")
}
func (w *deepSeekRecoveryStream) ToolExposed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.toolExposed
}
func (w *deepSeekRecoveryStream) ClientGone() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.clientGone
}
func (w *deepSeekRecoveryStream) BusinessStarted() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.business
}
func (w *deepSeekRecoveryStream) CanRetry() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.ended && w.err != nil && !w.toolExposed && !w.clientGone && !w.finalized
}
func (w *deepSeekRecoveryStream) Heartbeat() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.stream || w.finalized {
		return nil
	}
	return w.emit([]byte(": keep-alive\n\n"), false)
}

// Parser/source errors are retained for EndAttempt rather than returned from
// Write: forwarders interpret a Write error as a downstream disconnect.
func (w *deepSeekRecoveryStream) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.clientGone {
		return 0, w.clientErr
	}
	if !w.active || w.ended || w.finalized {
		return 0, errors.New("deepseek recovery: write outside attempt")
	}
	if w.size < 0 {
		w.size = 0
	}
	w.size += len(p)
	if w.err != nil || w.status < 200 || w.status >= 300 {
		return len(p), nil
	}
	if !w.stream {
		if len(p) > deepSeekRecoveryBodyLimit-w.body.Len() {
			w.err = errors.New("deepseek recovery: response too large")
		} else {
			_, _ = w.body.Write(p)
		}
		return len(p), nil
	}
	for _, b := range p {
		if w.skipLF {
			w.skipLF = false
			if b == '\n' {
				continue
			}
		}
		w.eventBytes++
		if w.eventBytes > deepSeekRecoveryEventLimit {
			w.err = errors.New("deepseek recovery: SSE event too large")
			break
		}
		if b == '\r' || b == '\n' {
			if b == '\r' {
				w.skipLF = true
			}
			if err := w.consumeLine(); err != nil {
				w.err = err
				break
			}
		} else {
			w.line = append(w.line, b)
		}
	}
	if w.clientGone {
		return 0, w.clientErr
	}
	return len(p), nil
}

func (w *deepSeekRecoveryStream) consumeLine() error {
	line := string(w.line)
	w.line = w.line[:0]
	if line != "" {
		if strings.HasPrefix(line, ":") {
			w.comment = true
			return nil
		}
		field, value, ok := strings.Cut(line, ":")
		if !ok {
			value = ""
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			w.event = value
		case "data":
			w.hasData = true
			w.data = append(w.data, []byte(value)...)
			w.data = append(w.data, '\n')
		}
		return nil
	}
	defer func() { w.event, w.data = "", w.data[:0]; w.eventBytes = 0; w.comment, w.hasData = false, false }()
	if !w.hasData {
		if w.comment {
			return w.emit([]byte(": keep-alive\n\n"), false)
		}
		return nil
	}
	data := bytes.TrimSuffix(w.data, []byte{'\n'})
	if w.protocol == "chat" && bytes.Equal(data, []byte("[DONE]")) {
		if !w.ccFinish || w.ccDone {
			return errors.New("deepseek recovery: unexpected DONE")
		}
		w.ccDone = true
		return nil
	}
	obj, err := deepSeekRecoveryObject(data)
	if err != nil {
		return err
	}
	if w.event == "error" || obj["error"] != nil || obj["type"] == "error" {
		return errors.New("deepseek recovery: upstream error event")
	}
	if w.protocol == "chat" {
		return w.chatFrame(obj)
	}
	return w.messagesFrame(obj)
}

func deepSeekRecoveryObject(data []byte) (map[string]any, error) {
	var obj map[string]any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&obj); err != nil || obj == nil {
		return nil, errors.New("deepseek recovery: invalid JSON response")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, errors.New("deepseek recovery: trailing JSON data")
	}
	return obj, nil
}
func deepSeekRecoveryCopy(obj map[string]any) map[string]any {
	out := make(map[string]any, len(obj))
	for k, v := range obj {
		out[k] = v
	}
	return out
}
func deepSeekRecoveryTool(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, v := range x {
			if v == nil {
				continue
			}
			if k == "tool_calls" || k == "function_call" || k == "tool_use" {
				return true
			}
			if k == "type" {
				if t, ok := v.(string); ok && (strings.Contains(t, "tool") || t == "input_json_delta" || strings.Contains(t, "function_call")) {
					return true
				}
			}
			if deepSeekRecoveryTool(v) {
				return true
			}
		}
	case []any:
		for _, v := range x {
			if deepSeekRecoveryTool(v) {
				return true
			}
		}
	}
	return false
}
func deepSeekRecoveryContent(obj map[string]any) bool {
	if deepSeekRecoveryTool(obj) {
		return true
	}
	for _, k := range []string{"content", "reasoning_content", "reasoning", "text", "thinking", "signature", "data", "refusal"} {
		switch v := obj[k].(type) {
		case string:
			if v != "" {
				return true
			}
		case []any:
			if len(v) > 0 {
				return true
			}
		}
	}
	return false
}

func (w *deepSeekRecoveryStream) chatFrame(obj map[string]any) error {
	if w.ccDone {
		return errors.New("deepseek recovery: data after DONE")
	}
	if typ, ok := obj["object"].(string); ok && typ != "chat.completion.chunk" {
		return errors.New("deepseek recovery: unexpected chat object")
	}
	choices, ok := obj["choices"].([]any)
	if !ok {
		return errors.New("deepseek recovery: missing chat choices")
	}
	if m, ok := obj["model"].(string); ok && w.model == "" {
		w.model = m
	}
	obj["id"], obj["created"] = "chatcmpl-"+w.id, w.created
	if w.model != "" {
		obj["model"] = w.model
	}
	live, ends := []any{}, []any{}
	business := false
	for _, raw := range choices {
		choice, ok := raw.(map[string]any)
		if !ok {
			return errors.New("deepseek recovery: invalid chat choice")
		}
		delta, _ := choice["delta"].(map[string]any)
		meaningful := deepSeekRecoveryContent(delta)
		if meaningful && w.ccFinish {
			return errors.New("deepseek recovery: content after finish_reason")
		}
		if reason, ok := choice["finish_reason"].(string); ok && reason != "" {
			switch reason {
			case "error", "aborted", "insufficient_system_resource":
				return errors.New("deepseek recovery: upstream generation failed")
			}
			w.ccFinish = true
			if reason == "tool_calls" || reason == "function_call" {
				w.ccToolFinish = true
			}
			end := deepSeekRecoveryCopy(choice)
			end["delta"] = map[string]any{}
			ends = append(ends, end)
		}
		if len(delta) > 0 {
			c := deepSeekRecoveryCopy(choice)
			c["finish_reason"] = nil
			live = append(live, c)
			business = business || meaningful
		}
	}
	if len(ends) > 0 || obj["usage"] != nil {
		end := deepSeekRecoveryCopy(obj)
		end["choices"] = ends
		if err := w.hold(deepSeekRecoveryFrame{data: end}, true); err != nil {
			return err
		}
	}
	if len(live) == 0 {
		return nil
	}
	chunk := deepSeekRecoveryCopy(obj)
	chunk["choices"] = live
	delete(chunk, "usage")
	frame := deepSeekRecoveryFrame{data: chunk}
	if !business && !w.attemptBusiness {
		return w.hold(frame, false)
	}
	if business {
		if err := w.startBusiness(); err != nil {
			return err
		}
	}
	return w.emitFrame(frame)
}

func (w *deepSeekRecoveryStream) messagesFrame(obj map[string]any) error {
	kind, _ := obj["type"].(string)
	if w.event != "" && w.event != kind {
		return errors.New("deepseek recovery: mismatched Messages event")
	}
	if kind == "ping" {
		return w.emit([]byte(": keep-alive\n\n"), false)
	}
	if w.messageStop {
		return errors.New("deepseek recovery: event after message_stop")
	}
	switch kind {
	case "message_start":
		if w.messageStart != nil {
			return errors.New("deepseek recovery: duplicate message_start")
		}
		msg, ok := obj["message"].(map[string]any)
		if !ok {
			return errors.New("deepseek recovery: invalid message_start")
		}
		if content, ok := msg["content"].([]any); ok && len(content) > 0 {
			return errors.New("deepseek recovery: nonempty message_start")
		}
		w.messageStart = msg
		w.messageUsage, _ = msg["usage"].(map[string]any)
		b, _ := json.Marshal(obj)
		if len(b) > deepSeekRecoveryPreludeLimit {
			return errors.New("deepseek recovery: message_start too large")
		}
		return nil
	case "message_delta":
		delta, _ := obj["delta"].(map[string]any)
		reason, _ := delta["stop_reason"].(string)
		if reason == "error" || reason == "aborted" || reason == "insufficient_system_resource" {
			return errors.New("deepseek recovery: upstream generation failed")
		}
		if reason != "" {
			w.messageDelta = true
		}
		if reason == "tool_use" && !w.attemptTool {
			return errors.New("deepseek recovery: tool terminal without tool")
		}
		copy := deepSeekRecoveryCopy(obj)
		usage := deepSeekRecoveryCopy(w.messageUsage)
		if tail, ok := obj["usage"].(map[string]any); ok {
			for k, v := range tail {
				usage[k] = v
			}
		}
		if len(usage) > 0 {
			copy["usage"] = usage
		}
		return w.hold(deepSeekRecoveryFrame{event: kind, data: copy}, true)
	case "message_stop":
		w.messageStop = true
		return w.hold(deepSeekRecoveryFrame{event: kind, data: obj}, true)
	case "content_block_start", "content_block_delta", "content_block_stop":
		if w.messageStart == nil || w.messageDelta {
			return errors.New("deepseek recovery: block outside message body")
		}
		n, ok := obj["index"].(json.Number)
		if !ok {
			return errors.New("deepseek recovery: missing block index")
		}
		index, err := n.Int64()
		if err != nil || index < 0 || index > 1<<30 {
			return errors.New("deepseek recovery: invalid block index")
		}
		block := w.blocks[int(index)]
		business := false
		switch kind {
		case "content_block_start":
			if block != nil || len(w.blocks) >= deepSeekRecoveryBlockLimit {
				return errors.New("deepseek recovery: duplicate or excessive blocks")
			}
			content, ok := obj["content_block"].(map[string]any)
			if !ok {
				return errors.New("deepseek recovery: missing content block")
			}
			typ, _ := content["type"].(string)
			if typ != "text" && typ != "thinking" && typ != "redacted_thinking" && !deepSeekRecoveryTool(content) {
				return errors.New("deepseek recovery: unsupported content block")
			}
			block = &deepSeekRecoveryBlock{kind: typ, index: -1, open: true}
			w.blocks[int(index)] = block
			business = deepSeekRecoveryContent(content)
		case "content_block_delta":
			if block == nil || !block.open {
				return errors.New("deepseek recovery: delta for closed or unknown block")
			}
			delta, ok := obj["delta"].(map[string]any)
			if !ok {
				return errors.New("deepseek recovery: missing block delta")
			}
			business = deepSeekRecoveryContent(delta)
		case "content_block_stop":
			if block == nil || !block.open {
				return errors.New("deepseek recovery: stop for closed or unknown block")
			}
			block.open = false
		}
		frame := deepSeekRecoveryFrame{event: kind, data: obj}
		if !business && !w.attemptBusiness {
			return w.hold(frame, false)
		}
		if business {
			if err := w.startBusiness(); err != nil {
				return err
			}
		}
		return w.emitMessageFrame(frame)
	default:
		return errors.New("deepseek recovery: unsupported Messages event")
	}
}

func (w *deepSeekRecoveryStream) hold(frame deepSeekRecoveryFrame, terminal bool) error {
	b, err := json.Marshal(frame.data)
	if err != nil {
		return err
	}
	if terminal {
		if len(b) > deepSeekRecoveryPreludeLimit-w.terminalBytes {
			return errors.New("deepseek recovery: terminal metadata too large")
		}
		w.terminalBytes += len(b)
		w.terminal = append(w.terminal, frame)
	} else {
		if len(b) > deepSeekRecoveryPreludeLimit-w.preludeBytes {
			return errors.New("deepseek recovery: prelude too large")
		}
		w.preludeBytes += len(b)
		w.prelude = append(w.prelude, frame)
	}
	return nil
}
func (w *deepSeekRecoveryStream) ensureMessage() error {
	if w.messageEmitted {
		return nil
	}
	if w.messageStart == nil {
		return errors.New("deepseek recovery: missing message_start")
	}
	msg := deepSeekRecoveryCopy(w.messageStart)
	msg["id"] = "msg_" + w.id
	// Initial usage is pending; the selected attempt's actual usage is released
	// in message_delta. Failed-attempt usage must not leak into the logical reply.
	msg["usage"] = map[string]any{"input_tokens": 0, "output_tokens": 0}
	w.messageEmitted = true
	return w.emitFrame(deepSeekRecoveryFrame{event: "message_start", data: map[string]any{"type": "message_start", "message": msg}})
}
func (w *deepSeekRecoveryStream) startBusiness() error {
	if w.attemptBusiness {
		return nil
	}
	if w.protocol == "messages" {
		if err := w.ensureMessage(); err != nil {
			return err
		}
	}
	if w.pendingNotice {
		w.pendingNotice = false
		if w.protocol == "chat" {
			frame := map[string]any{"id": "chatcmpl-" + w.id, "object": "chat.completion.chunk", "created": w.created, "model": w.model,
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": deepSeekRecoveryNotice}, "finish_reason": nil}}}
			if err := w.emitFrame(deepSeekRecoveryFrame{data: frame}); err != nil {
				return err
			}
		} else {
			index := w.nextIndex
			w.nextIndex++
			frames := []deepSeekRecoveryFrame{
				{event: "content_block_start", data: map[string]any{"type": "content_block_start", "index": index, "content_block": map[string]any{"type": "text", "text": ""}}},
				{event: "content_block_delta", data: map[string]any{"type": "content_block_delta", "index": index, "delta": map[string]any{"type": "text_delta", "text": deepSeekRecoveryNotice}}},
				{event: "content_block_stop", data: map[string]any{"type": "content_block_stop", "index": index}},
			}
			for _, f := range frames {
				if err := w.emitFrame(f); err != nil {
					return err
				}
			}
		}
	}
	if err := w.flushPrelude(); err != nil {
		return err
	}
	w.business, w.attemptBusiness = true, true
	return nil
}
func (w *deepSeekRecoveryStream) flushPrelude() error {
	if w.protocol == "messages" && len(w.prelude) > 0 {
		if err := w.ensureMessage(); err != nil {
			return err
		}
	}
	for _, frame := range w.prelude {
		var err error
		if w.protocol == "messages" {
			err = w.emitMessageFrame(frame)
		} else {
			err = w.emitFrame(frame)
		}
		if err != nil {
			return err
		}
	}
	w.prelude = nil
	w.preludeBytes = 0
	return nil
}
func (w *deepSeekRecoveryStream) emitMessageFrame(frame deepSeekRecoveryFrame) error {
	n, _ := frame.data["index"].(json.Number)
	index, _ := n.Int64()
	block := w.blocks[int(index)]
	if block == nil {
		return errors.New("deepseek recovery: unmapped content block")
	}
	if frame.event == "content_block_start" {
		block.index = w.nextIndex
		w.nextIndex++
		block.exposedOpen = true
	}
	if block.index < 0 {
		return errors.New("deepseek recovery: block not started")
	}
	copy := deepSeekRecoveryCopy(frame.data)
	copy["index"] = block.index
	if frame.event == "content_block_stop" {
		block.exposedOpen = false
	}
	frame.data = copy
	return w.emitFrame(frame)
}
func (w *deepSeekRecoveryStream) closeNonToolBlocks() error {
	open := []*deepSeekRecoveryBlock{}
	for _, block := range w.blocks {
		if block.exposedOpen {
			if strings.Contains(block.kind, "tool") {
				return errors.New("deepseek recovery: exposed tool block")
			}
			open = append(open, block)
		}
	}
	sort.Slice(open, func(i, j int) bool { return open[i].index < open[j].index })
	for _, block := range open {
		block.exposedOpen = false
		if err := w.emitFrame(deepSeekRecoveryFrame{event: "content_block_stop", data: map[string]any{"type": "content_block_stop", "index": block.index}}); err != nil {
			return err
		}
	}
	return nil
}
func (w *deepSeekRecoveryStream) emitFrame(frame deepSeekRecoveryFrame) error {
	payload, err := json.Marshal(frame.data)
	if err != nil {
		return err
	}
	var b []byte
	if frame.event != "" {
		b = append(b, []byte("event: "+frame.event+"\n")...)
	}
	b = append(b, []byte("data: ")...)
	b = append(b, payload...)
	b = append(b, '\n', '\n')
	return w.emit(b, deepSeekRecoveryTool(frame.data))
}
func (w *deepSeekRecoveryStream) emit(p []byte, tool bool) error {
	if w.clientGone {
		return w.clientErr
	}
	if tool {
		w.toolExposed, w.attemptTool = true, true
	}
	if !w.downstream.Written() {
		contentType := "application/json"
		if w.stream {
			contentType = "text/event-stream"
			w.downstream.Header().Set("Cache-Control", "no-cache")
			w.downstream.Header().Set("X-Accel-Buffering", "no")
		}
		w.downstream.Header().Set("Content-Type", contentType)
		w.downstream.WriteHeader(http.StatusOK)
	}
	n, err := w.downstream.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.clientGone, w.clientErr = true, err
		return err
	}
	if w.stream {
		w.downstream.Flush()
	}
	return nil
}

func (w *deepSeekRecoveryStream) EndAttempt(upstreamErr error) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ended {
		return w.err
	}
	if !w.active || w.finalized {
		return errors.New("deepseek recovery: no active attempt")
	}
	if w.status < 200 || w.status >= 300 {
		w.err = errors.Join(w.err, fmt.Errorf("deepseek recovery: upstream HTTP %d", w.status))
	}
	if w.clientGone {
		w.err = errors.Join(w.err, w.clientErr)
	}
	if w.err == nil && upstreamErr == nil {
		if w.stream {
			if len(w.line) > 0 || w.hasData {
				w.err = errors.New("deepseek recovery: incomplete SSE event")
			} else if w.protocol == "chat" {
				if !w.ccFinish {
					w.err = errors.New("deepseek recovery: missing chat terminal")
				}
				if w.ccToolFinish && !w.attemptTool {
					w.err = errors.New("deepseek recovery: tool terminal without tool")
				}
			} else {
				if w.messageStart == nil || !w.messageDelta || !w.messageStop {
					w.err = errors.New("deepseek recovery: missing Messages terminal")
				}
				for _, block := range w.blocks {
					if block.open {
						w.err = errors.New("deepseek recovery: unclosed content block")
						break
					}
				}
			}
		} else {
			obj, err := deepSeekRecoveryObject(w.body.Bytes())
			w.err = err
			if err == nil && (obj["error"] != nil || obj["type"] == "error") {
				w.err = errors.New("deepseek recovery: upstream JSON error")
			}
		}
	}
	w.err = errors.Join(w.err, upstreamErr)
	w.ended = true
	return w.err
}

func (w *deepSeekRecoveryStream) CommitSuccess() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.finalized {
		return errors.New("deepseek recovery: already finalized")
	}
	if !w.ended || w.err != nil {
		return errors.New("deepseek recovery: cannot commit unsuccessful attempt")
	}
	w.finalized = true
	if !w.stream {
		obj, _ := deepSeekRecoveryObject(w.body.Bytes())
		return w.emit(w.body.Bytes(), deepSeekRecoveryTool(obj))
	}
	if w.protocol == "messages" {
		if err := w.ensureMessage(); err != nil {
			return err
		}
	}
	if err := w.flushPrelude(); err != nil {
		return err
	}
	for _, frame := range w.terminal {
		if err := w.emitFrame(frame); err != nil {
			return err
		}
	}
	if w.protocol == "chat" {
		return w.emit([]byte("data: [DONE]\n\n"), false)
	}
	return nil
}

func (w *deepSeekRecoveryStream) CommitFailure(message string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.finalized {
		return errors.New("deepseek recovery: already finalized")
	}
	w.finalized = true
	if w.clientGone {
		return w.clientErr
	}
	if message == "" {
		message = "DeepSeek 暂时无法完成回复，请稍后重试。"
	}
	obj := map[string]any{"error": map[string]any{"type": "upstream_error", "code": "upstream_error", "message": message}}
	event := ""
	if w.protocol == "messages" {
		event = "error"
		obj = map[string]any{"type": "error", "error": map[string]any{"type": "api_error", "message": message}}
	}
	if w.stream && w.downstream.Written() {
		return w.emitFrame(deepSeekRecoveryFrame{event: event, data: obj})
	}
	b, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	w.downstream.Header().Set("Content-Type", "application/json")
	w.downstream.WriteHeader(http.StatusBadGateway)
	// WriteHeaderNow preserves 502; emit normally initializes fresh streams as 200.
	w.downstream.WriteHeaderNow()
	return w.emit(b, false)
}
