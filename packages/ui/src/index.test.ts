import { describe, it, expect } from "vitest";
import * as ui from "./index";

describe("ui barrel exports", () => {
  it("re-exports every component as a defined value", () => {
    const names = [
      "Button",
      "Card",
      "Drawer",
      "EmptyState",
      "Icon",
      "LocationBar",
      "MealCard",
      "Modal",
      "PageHeader",
      "ProviderButton",
      "SearchInput",
      "StateTag",
      "StatCard",
      "Tabs",
      "TBiteLogo",
      "Toggle",
      "WeekCalendar",
    ] as const;
    for (const name of names) {
      expect(ui[name], name).toBeTruthy();
    }
  });
});
