import { describe, expect, it } from "vitest";
import { cn, fontSizes } from "./cn";

describe("cn", () => {
  it("keeps a custom font size next to a text color", () => {
    for (const size of fontSizes) {
      expect(cn(`text-${size}`, "text-muted")).toBe(`text-${size} text-muted`);
      expect(cn("text-muted", `text-${size}`)).toBe(`text-muted text-${size}`);
    }
  });

  it("lets a later font size override an earlier one", () => {
    expect(cn("text-13 font-medium", "text-14")).toBe("font-medium text-14");
    expect(cn("text-12", "text-sm")).toBe("text-sm");
  });

  it("lets a later text color override an earlier one", () => {
    expect(cn("text-14 text-muted", "text-fg")).toBe("text-14 text-fg");
  });

  it("merges the custom radii and shadow with the built-in ones", () => {
    expect(cn("rounded-full", "rounded-inner")).toBe("rounded-inner");
    expect(cn("rounded-container", "rounded-none")).toBe("rounded-none");
    expect(cn("shadow-float", "shadow-none")).toBe("shadow-none");
  });
});
