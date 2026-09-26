package basispoints

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Codex delivers these calls as encrypted agent messages when the upstream
// marks a parameter encrypted. BPS history cannot consume that delivery.
func validateCollaborationEncryption(native object, info tool) error {
	name := info.Name
	if info.Namespace != "" {
		name = info.Namespace + "." + name
	}
	name = strings.TrimPrefix(name, "functions.")
	switch name {
	case "collaboration.spawn_agent", "collaboration.send_message", "collaboration.followup_task":
	default:
		return nil
	}
	metadata, present := native["encrypted_function_args"]
	if !present || metadata == nil {
		return nil
	}
	raw, err := json.Marshal(metadata)
	if err == nil && string(raw) == "[]" {
		return nil
	}
	return fmt.Errorf("basispoints cannot relay encrypted collaboration arguments; retry the same operation with plaintext arguments through run_officejs; no client tool was executed")
}
