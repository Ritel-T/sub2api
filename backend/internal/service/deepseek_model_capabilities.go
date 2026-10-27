package service

import "context"

// DeepSeekModelImageInputsForGroup shares the Codex catalog's mapped-model,
// group restriction and all-candidate intersection rules with ordinary /models.
// Missing accounts or a failed lookup leave every entry text-only.
func (s *GatewayService) DeepSeekModelImageInputsForGroup(ctx context.Context, group *Group, modelIDs []string) map[string]bool {
	images := make(map[string]bool, len(modelIDs))
	if s == nil || s.accountRepo == nil || group == nil || group.Platform != PlatformDeepseek {
		return images
	}
	_, accounts, err := loadCodexGroupCatalogAccounts(ctx, s.accountRepo, group.ID)
	if err != nil {
		return images
	}
	for _, modelID := range modelIDs {
		modelAccounts := accountsAllowedInGroupForModel(accounts, &group.ID, modelID)
		images[modelID] = groupCodexModelSupportsImageInput(PlatformDeepseek, modelID, modelAccounts, nil, true)
	}
	return images
}
