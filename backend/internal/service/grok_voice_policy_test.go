package service

import (
	"net/url"
	"testing"
)

func TestGrokVoicePolicyDisablesCustomVoiceObjects(t *testing.T) {
	for _, endpoint := range []string{"custom-voices", "custom-voices/shared123", "custom-voices/shared123/audio", "/custom-voices/shared123/"} {
		if err := ValidateGrokVoiceRequest(endpoint, nil); err != ErrGrokCustomVoiceUnavailable {
			t.Errorf("%s: got %v", endpoint, err)
		}
	}
	if err := ValidateGrokVoiceRequest("stt", []byte("multipart audio data")); err != nil {
		t.Fatal(err)
	}
}

func TestGrokVoicePolicyAllowsEveryPublicBuiltin(t *testing.T) {
	if len(grokBuiltinVoiceIDs) != 28 {
		t.Fatalf("unexpected public voice count: %d", len(grokBuiltinVoiceIDs))
	}
	for voice := range grokBuiltinVoiceIDs {
		for _, field := range []string{"voice", "voice_id"} {
			if err := ValidateGrokVoiceRequest("tts", []byte(`{"text":"hello","`+field+`":"`+voice+`"}`)); err != nil {
				t.Fatalf("%s: %v", voice, err)
			}
			if err := ValidateGrokVoiceQuery(url.Values{field: {voice}}); err != nil {
				t.Fatalf("%s: %v", voice, err)
			}
		}
	}
	for _, body := range []string{`{"text":"hello"}`, `{"voice_id":"Ara"}`, `{"voice":"EVE"}`, `{"type":"session.update","session":{"voice":"eve"}}`, `{"type":"response.create","response":{"audio":{"output":{"voice":"carina"}}}}`, `{"type":"text.delta","delta":"voice_id: shared123"}`, `{"type":"session.update","session":{"tools":[{"parameters":{"properties":{"voice":{"type":"string"}}}}]}}`, `{"type":"conversation.item.create","item":{"content":[{"voice":"shared123"}]}}`} {
		if err := validateGrokVoiceJSON([]byte(body)); err != nil {
			t.Fatalf("%s: %v", body, err)
		}
	}
	if err := ValidateGrokVoiceQuery(nil); err != nil {
		t.Fatal(err)
	}
}

func TestGrokVoicePolicyRejectsPrivateControlsAndParserAmbiguity(t *testing.T) {
	for _, body := range []string{
		`{"voice_id":"shared123"}`, `{"voice":"shared123"}`, `{"voice":"eve","voice_id":"shared123"}`,
		`{"voice":null}`, `{"voice":{"id":"eve"}}`, `{"voice":["eve"]}`,
		`{"type":"session.update","session":{"voice":"shared123"}}`,
		`{"type":"response.create","response":{"audio":{"output":{"voice":"shared123"}}}}`,
		`{"session":{"audio":{"output":{"voice_id":"shared123"}}}}`,
		`{"voice_id":"shared123","voice_id":"eve"}`, `{"voice_id":"eve","voice_id":"shared123"}`,
		`{"voice_id":"shared123","voice_\u0069d":"eve"}`, `{"session":{"voice":"shared123"},"session":{"voice":"eve"}}`,
		`{"voice":"eve"} {"voice":"shared123"}`, `[]`, `null`, `{"voice":"eve"`,
	} {
		if err := validateGrokVoiceJSON([]byte(body)); err == nil {
			t.Fatalf("unsafe voice control accepted: %s", body)
		}
	}
	for _, query := range []url.Values{
		{"voice": {"shared123"}}, {"voice": {"eve", "shared123"}}, {"voice": {"shared123", "eve"}},
		{"voice": {"eve"}, "voice_id": {"shared123"}}, {"Voice": {"shared123"}},
	} {
		if err := ValidateGrokVoiceQuery(query); err != ErrGrokCustomVoiceUnavailable {
			t.Fatalf("query %v: got %v", query, err)
		}
	}
}
