import { describe, expect, it } from "vitest";

import { defaultAllowImageGenerationForPlatform, imagePricingPlatforms, supportsImagePricingPlatform, supportsVideoPricingPlatform } from "../groupsImagePricing";

describe("groups image pricing platform support", () => {
  it("includes Grok image groups", () => {
    expect(supportsImagePricingPlatform("grok")).toBe(true);
    expect(imagePricingPlatforms.has("grok")).toBe(true);
  });

  it("enables video pricing controls for Grok only", () => {
    expect(supportsVideoPricingPlatform("grok")).toBe(true);
    expect(supportsVideoPricingPlatform("openai")).toBe(false);
  });

  it("keeps non-media group platforms out of the image pricing controls", () => {
    expect(supportsImagePricingPlatform("anthropic")).toBe(false);
  });

  it("defaults image generation on for Grok and Gemini-family groups only", () => {
    expect(defaultAllowImageGenerationForPlatform("grok")).toBe(true);
    expect(defaultAllowImageGenerationForPlatform("gemini")).toBe(true);
    expect(defaultAllowImageGenerationForPlatform("antigravity")).toBe(true);
    expect(defaultAllowImageGenerationForPlatform("composite")).toBe(false);
    expect(defaultAllowImageGenerationForPlatform("openai")).toBe(false);
  });

  it("includes Composite groups in image pricing controls", () => {
    expect(supportsImagePricingPlatform("composite")).toBe(true);
    expect(imagePricingPlatforms.has("composite")).toBe(true);
  });
});
