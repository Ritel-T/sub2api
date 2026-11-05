package service

import (
	"encoding/json"
	"fmt"
	"regexp"
)

const SettingKeyQuickKeyDefaultGroups = "quick_key_default_groups"

// Public IDs guide quick creation only; they never grant group access.
type QuickKeyDefaultGroups map[string]int64

var quickKeyBucket = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func ValidateQuickKeyDefaultGroups(groups QuickKeyDefaultGroups) error {
	for platform, id := range groups {
		if !quickKeyBucket.MatchString(platform) || id <= 0 || id > 9007199254740991 {
			return fmt.Errorf("quick_key_default_groups requires platform keys and positive safe integer group IDs")
		}
	}
	return nil
}

func parseQuickKeyDefaultGroups(raw string) QuickKeyDefaultGroups {
	groups := QuickKeyDefaultGroups{}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &groups); err != nil || ValidateQuickKeyDefaultGroups(groups) != nil {
			return QuickKeyDefaultGroups{}
		}
	}
	if groups == nil {
		return QuickKeyDefaultGroups{}
	}
	return groups
}
