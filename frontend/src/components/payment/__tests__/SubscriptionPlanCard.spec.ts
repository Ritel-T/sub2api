import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import { createPinia } from "pinia";
import { createI18n } from "vue-i18n";
import type { SubscriptionPlan } from "@/types/payment";
import SubscriptionPlanCard from "../SubscriptionPlanCard.vue";

const i18n = createI18n({
  legacy: false,
  locale: "en",
  fallbackWarn: false,
  missingWarn: false,
  messages: {
    en: {
      payment: {
        days: "days",
        weeks: "weeks",
        months: "months",
        perMonth: "month",
        models: "Models",
        planCard: {
          quota: "Quota",
          rate: "Rate",
          unlimited: "Unlimited",
        },
        subscribeNow: "Subscribe now",
      },
    },
  },
});

const mountPlanCard = (groupPlatform: string, overrides: Partial<SubscriptionPlan> = {}) =>
  mount(SubscriptionPlanCard, {
    props: {
      plan: {
        id: 1,
        group_id: 10,
        group_platform: groupPlatform,
        name: "Pro",
        price: 10,
        amount: 1000,
        features: [],
        rate_multiplier: 1,
        validity_days: 30,
        validity_unit: "day",
        supported_model_scopes: ["claude", "gemini_text", "gemini_image"],
        is_active: true,
        ...overrides,
      },
    },
    global: { plugins: [i18n, createPinia()] },
  });

describe("SubscriptionPlanCard", () => {
  it("does not show Antigravity model scopes for OpenAI plans", () => {
    const text = mountPlanCard("openai").text();

    expect(text).not.toContain("Claude");
    expect(text).not.toContain("Gemini");
    expect(text).not.toContain("Imagen");
  });

  it("shows model scopes for Antigravity plans", () => {
    const text = mountPlanCard("antigravity").text();

    expect(text).toContain("Claude");
    expect(text).toContain("Gemini");
    expect(text).toContain("Imagen");
  });

  // #4607：管理端保存的单位是复数（months/weeks），此前用户侧只匹配单数
  // 'month'，「1 个月」的套餐卡片被显示成「1天」。测试环境的 vue-i18n 为
  // runtime-only 构建，t() 原样返回 key，故按 key 断言单位分支。
  it("renders plural admin-form validity units instead of mislabeled days (#4607)", () => {
    expect(mountPlanCard("openai", { validity_days: 1, validity_unit: "months" }).text()).toContain("/ payment.perMonth");
    expect(mountPlanCard("openai", { validity_days: 3, validity_unit: "months" }).text()).toContain("/ 3payment.months");
    expect(mountPlanCard("openai", { validity_days: 2, validity_unit: "weeks" }).text()).toContain("/ 2payment.weeks");
    expect(mountPlanCard("openai", { validity_days: 30, validity_unit: "day" }).text()).toContain("/ 30payment.days");
  });

  it("uses the configured currency symbol while preserving USD for legacy plans", () => {
    const cnyPlan = mountPlanCard("openai", { currency: "CNY", original_price: 20 }).text();

    expect(cnyPlan).toContain("¥10CNY");
    expect(cnyPlan).toContain("¥20CNY");
    expect(mountPlanCard("openai", { currency: "USD" }).text()).toContain("$10USD");
    expect(mountPlanCard("openai", { currency: "" }).text()).toContain("$10");
  });

  it.each([
    ["long Chinese", "企业全球加速专业订阅套餐（含高级模型与优先支持）"],
    ["long English", "Enterprise Global Acceleration Subscription with Priority Support"],
    ["unbroken token", "EnterpriseGlobalAccelerationSubscriptionWithPrioritySupport1234567890"],
  ])("keeps the full %s plan title accessible", (_label, name) => {
    const wrapper = mountPlanCard("openai", { name });
    const title = wrapper.get("h3");

    expect(title.text()).toBe(name);
    expect(title.attributes("title")).toBe(name);
  });

  it("keeps identity, terms and the actionable purchase offer available", () => {
    const wrapper = mountPlanCard("openai", {
      name: "Enterprise Global Acceleration Subscription with Priority Support",
      price: 123.45,
      currency: "USD",
      description: "Includes advanced models and priority support.",
    });
    const title = wrapper.get("h3");
    const badge = wrapper.findAll("span").find((node) => node.text() === "OpenAI");
    const price = wrapper.findAll("span").find((node) => node.text() === "123.45");

    expect(title.text()).toBe("Enterprise Global Acceleration Subscription with Priority Support");
    expect(badge?.text()).toBe("OpenAI");
    expect(price?.text()).toBe("123.45");
    expect(badge?.element.parentElement?.textContent).toContain("/ 30payment.days");
    expect(wrapper.get("p").text()).toBe("Includes advanced models and priority support.");
    expect(wrapper.get("button").text()).toBe("payment.subscribeNow");
  });

  it("keeps short plan identities and validity readable", () => {
    const wrapper = mountPlanCard("openai", { name: "Pro", description: "" });
    const title = wrapper.get("h3");
    const badge = wrapper.findAll("span").find((node) => node.text() === "OpenAI");

    expect(title.text()).toBe("Pro");
    expect(title.attributes("title")).toBe("Pro");
    expect(badge?.element.parentElement?.textContent).toContain("/ 30payment.days");
  });
});


describe("SubscriptionPlanCard live group terms", () => {
  it("keeps product identity and custom benefits while reading current group quotas", async () => {
    const wrapper = mountPlanCard("openai", {
      name: "Commercial product", group_name: "Current group", group_status: "active", for_sale: true,
      daily_limit_usd: 10, weekly_limit_usd: 70, monthly_limit_usd: 1120,
      features: ["月额度：$280", "Priority support", "每日客服支持"],
    });
    expect(wrapper.get("h3").text()).toBe("Commercial product");
    expect(wrapper.get('[data-testid="plan-group-name"]').text()).toContain("Current group");
    expect(wrapper.text()).toContain("$1120");
    expect(wrapper.text()).not.toContain("$280");
    expect(wrapper.text()).toContain("Priority support");
    expect(wrapper.text()).toContain("每日客服支持");
    await wrapper.setProps({ plan: { ...wrapper.props("plan"), group_name: "Renamed group", monthly_limit_usd: 2240 } });
    expect(wrapper.text()).toContain("$2240");
    expect(wrapper.text()).not.toContain("$1120");
    expect(wrapper.get('[data-testid="plan-group-name"]').text()).toContain("Renamed group");
  });

  it("shows unlimited for zero and absent caps instead of a zero allowance", () => {
    const wrapper = mountPlanCard("openai", { daily_limit_usd: 0, weekly_limit_usd: -1, monthly_limit_usd: null });
    expect(wrapper.text()).toContain("payment.planCard.unlimited");
    expect(wrapper.text()).not.toContain("$0");
  });

  it.each([{ for_sale: false }, { group_status: "inactive" }])("does not select unavailable stale plan %j", async (overrides) => {
    const wrapper = mountPlanCard("openai", { for_sale: true, group_status: "active", ...overrides });
    expect(wrapper.get("button").attributes("disabled")).toBeDefined();
    await wrapper.get("button").trigger("click");
    expect(wrapper.emitted("select")).toBeUndefined();
  });
});
