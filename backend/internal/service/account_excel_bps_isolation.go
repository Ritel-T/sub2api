package service

import (
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	ExcelBPSRequiredGroupIDsKey = "openai_excel_bps_required_group_ids"
	ExcelBPSRequiredModelsKey   = "openai_excel_bps_required_models"
)

// The required policy is independent of the protocol enable switch/model list.
// Turning BPS off (including an automatic 403 action) must never make a protected
// model available over Codex. Other models remain native in every group.
func (a *Account) excelBPSGroupIsolation() (groups []int64, active, valid bool) {
	if a == nil {
		return nil, false, true
	}
	raw := a.Extra[ExcelBPSRequiredGroupIDsKey]
	if raw == nil {
		return nil, false, true
	}
	var values []any
	switch ids := raw.(type) {
	case []int64:
		for _, id := range ids {
			values = append(values, id)
		}
	case []int:
		for _, id := range ids {
			values = append(values, id)
		}
	case []any:
		values = ids
	default:
		return nil, true, false
	}
	for _, value := range values {
		id, ok := excelBPS403GroupID(value)
		if !ok || id <= 0 {
			return nil, true, false
		}
		groups = append(groups, id)
	}
	return groups, len(groups) > 0, true
}

func excelBPSModelList(raw any) ([]string, bool) {
	var values []string
	switch models := raw.(type) {
	case []string:
		values = models
	case []any:
		for _, model := range models {
			value, ok := model.(string)
			if !ok {
				return nil, false
			}
			values = append(values, value)
		}
	default:
		return nil, false
	}
	models := make([]string, 0, len(values))
	for _, model := range values {
		model = strings.TrimSpace(model)
		if model == "" || strings.Contains(model, "*") {
			return nil, false
		}
		models = append(models, normalizeExcelBPSIsolationModel(model))
	}
	return models, len(models) > 0
}

// Keep isolation conservative for effort aliases, including new model families
// that have not yet reached the legacy Codex alias table.
func normalizeExcelBPSIsolationModel(model string) string {
	model = normalizeCodexModel(model)
	for _, suffix := range []string{"-none", "-minimal", "-low", "-medium", "-high", "-xhigh", "-max", "-ultra"} {
		if base, ok := strings.CutSuffix(model, suffix); ok {
			return normalizeCodexModel(base)
		}
	}
	if len(model) > 11 && model[len(model)-11] == '-' && isCodexDateSuffix(model[len(model)-10:]) {
		return normalizeCodexModel(model[:len(model)-11])
	}
	return model
}

func (a *Account) excelBPSRequiredUpstreamModel(model string) bool {
	_, active, valid := a.excelBPSGroupIsolation()
	if !active {
		return false
	}
	models, modelValid := excelBPSModelList(a.Extra[ExcelBPSRequiredModelsKey])
	if !valid || !modelValid {
		return true
	} // corrupted policy fails closed
	model = normalizeExcelBPSIsolationModel(model)
	for _, required := range models {
		if model == required {
			return true
		}
	}
	return false
}

func (a *Account) excelBPSRequiredForModel(model string) bool {
	return a != nil && (a.excelBPSRequiredUpstreamModel(model) || a.excelBPSRequiredUpstreamModel(a.GetMappedModel(model)))
}

func (a *Account) excelBPSModelAllowedInGroup(groupID *int64, model string) bool {
	groups, active, valid := a.excelBPSGroupIsolation()
	if !active {
		return true
	}
	if !valid {
		return false
	}
	if !a.excelBPSRequiredForModel(model) {
		return true
	}
	if groupID == nil || !containsInt64(groups, *groupID) {
		return false
	}
	return a.IsExcelBPSEnabledForModel(model)
}

func validateExcelBPSGroupIsolationExtra(extra map[string]any) error {
	a := &Account{Extra: extra}
	_, active, valid := a.excelBPSGroupIsolation()
	if !valid {
		return infraerrors.BadRequest("OPENAI_EXCEL_BPS_INVALID", ExcelBPSRequiredGroupIDsKey+" must be an array of positive group IDs or null")
	}
	if active {
		if _, valid := excelBPSModelList(extra[ExcelBPSRequiredModelsKey]); !valid {
			return infraerrors.BadRequest("OPENAI_EXCEL_BPS_INVALID", ExcelBPSRequiredModelsKey+" must contain explicit model names while group isolation is enabled")
		}
	}
	return nil
}
