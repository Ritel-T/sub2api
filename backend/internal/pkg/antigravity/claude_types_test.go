package antigravity

import (
	"encoding/json"
	"testing"
)

func TestClaudeContentItem_ThinkingAlwaysHasSignature(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(ClaudeContentItem{Type: "thinking", Thinking: "plan"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), `{"thinking":"plan","signature":"","type":"thinking"}`; got != want {
		t.Fatalf("thinking block = %s, want %s", got, want)
	}

	data, err = json.Marshal(ClaudeContentItem{Type: "text", Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), `{"type":"text","text":"hi"}`; got != want {
		t.Fatalf("text block = %s, want %s", got, want)
	}
}

func TestIsGeminiReasoningModel_IncludesProAgent(t *testing.T) {
	t.Parallel()

	if !IsGeminiReasoningModel("gemini-pro-agent") {
		t.Fatal("expected gemini-pro-agent to be recognized as a Gemini reasoning model")
	}
	if !IsGeminiReasoningModel("gemini-3.8-flash-high") {
		t.Fatal("expected gemini-3.8-flash-high to be recognized as a Gemini reasoning model")
	}
	if !IsGeminiReasoningModel("gemini-4-argon") {
		t.Fatal("expected gemini-4-argon to be recognized as a Gemini reasoning model")
	}
	if !IsGeminiReasoningModel("gemini-4-argon-high") {
		t.Fatal("expected gemini-4-argon-high to be recognized as a Gemini reasoning model")
	}
	if IsGeminiReasoningModel("gemini-2.5-flash") {
		t.Fatal("did not expect gemini-2.5-flash to be recognized as a Gemini reasoning model")
	}
}
