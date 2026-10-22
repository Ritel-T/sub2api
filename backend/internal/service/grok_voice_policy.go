package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
)

// ErrGrokCustomVoiceUnavailable is a temporary fail-closed boundary until
// private voices have durable tenant ownership and exact upstream-account affinity.
var ErrGrokCustomVoiceUnavailable = errors.New("custom voices are unavailable; use a built-in voice")

// Official built-in IDs, verified against the xAI Voice documentation on 2026-10-22:
// https://docs.x.ai/developers/model-capabilities/audio/voice
// Never infer public ownership from the shape or length of a caller-supplied ID.
var grokBuiltinVoiceIDs = map[string]struct{}{
	"carina": {}, "zagan": {}, "helix": {}, "orion": {}, "luna": {},
	"iris": {}, "altair": {}, "zenith": {}, "perseus": {}, "helios": {},
	"lux": {}, "kepler": {}, "rigel": {}, "cosmo": {}, "celeste": {},
	"ursa": {}, "sirius": {}, "lumen": {}, "castor": {}, "naksh": {},
	"atlas": {}, "aurora": {}, "liora": {}, "ara": {}, "eve": {},
	"leo": {}, "rex": {}, "sal": {},
}

func validateGrokBuiltinVoice(value any) error {
	id, ok := value.(string)
	if !ok {
		return ErrGrokCustomVoiceUnavailable
	}
	if _, ok := grokBuiltinVoiceIDs[strings.ToLower(strings.TrimSpace(id))]; !ok {
		return ErrGrokCustomVoiceUnavailable
	}
	return nil
}

// ValidateGrokVoiceRequest runs before credentials, billing reservations or
// account selection. STT's multipart body is not a voice-selection surface.
func ValidateGrokVoiceRequest(endpoint string, body []byte) error {
	endpoint = strings.Trim(strings.TrimSpace(endpoint), "/")
	if endpoint == "custom-voices" || strings.HasPrefix(endpoint, "custom-voices/") {
		return ErrGrokCustomVoiceUnavailable
	}
	if endpoint == "tts" {
		return validateGrokVoiceJSON(body)
	}
	return nil
}

// ValidateGrokVoiceQuery examines every occurrence, not Values.Get's first
// value, so duplicate query parameters cannot bypass the policy.
func ValidateGrokVoiceQuery(query url.Values) error {
	for key, values := range query {
		if !strings.EqualFold(key, "voice") && !strings.EqualFold(key, "voice_id") {
			continue
		}
		for _, value := range values {
			if err := validateGrokBuiltinVoice(value); err != nil {
				return err
			}
		}
	}
	return nil
}

// Validate voice controls only. User text, function schemas and tool results
// may contain arbitrary fields named voice and are not authorization controls.
func validateGrokVoiceControls(obj map[string]any) error {
	for key, value := range obj {
		switch strings.ToLower(key) {
		case "voice", "voice_id":
			if err := validateGrokBuiltinVoice(value); err != nil {
				return err
			}
		case "session", "response", "audio", "output":
			if nested, ok := value.(map[string]any); ok {
				if err := validateGrokVoiceControls(nested); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func validateGrokVoiceJSON(body []byte) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	value, err := decodeGrokVoiceValue(dec, 0)
	if err != nil {
		return fmt.Errorf("invalid voice request: %w", err)
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return errors.New("voice request must be a JSON object")
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("voice request must contain one JSON object")
	}
	return validateGrokVoiceControls(obj)
}

// Reject duplicate keys instead of assuming the upstream decoder chooses the
// same occurrence as this process. Token decoding also preserves JSON numbers.
func decodeGrokVoiceValue(dec *json.Decoder, depth int) (any, error) {
	if depth > 64 {
		return nil, errors.New("voice request nesting is too deep")
	}
	token, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return token, nil
	}
	switch delim {
	case '{':
		obj := make(map[string]any)
		for dec.More() {
			token, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key, ok := token.(string)
			if !ok {
				return nil, errors.New("invalid object key")
			}
			if _, exists := obj[key]; exists {
				return nil, errors.New("duplicate JSON key")
			}
			value, err := decodeGrokVoiceValue(dec, depth+1)
			if err != nil {
				return nil, err
			}
			obj[key] = value
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return obj, nil
	case '[':
		var values []any
		for dec.More() {
			value, err := decodeGrokVoiceValue(dec, depth+1)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return values, nil
	default:
		return nil, errors.New("unexpected JSON delimiter")
	}
}
