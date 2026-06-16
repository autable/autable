import { describe, expect, it } from "vitest";
import { renderFormScript } from "./formRuntime";

describe("renderFormScript", () => {
  it("executes form JavaScript against the form api and root", () => {
    const result = renderFormScript(`
      const email = api.input({ name: "email", label: "Email", type: "email", required: true });
      const status = api.select({ name: "status", options: ["Active", "Review"] });
      root.append(email, status, api.submit("Create record"));
    `);

    expect(result.error).toBeUndefined();
    expect(result.elements).toEqual([
      { kind: "input", name: "email", label: "Email", inputType: "email", required: true },
      { kind: "select", name: "status", label: "status", options: ["Active", "Review"] },
      { kind: "submit", label: "Create record" }
    ]);
  });

  it("allows submit buttons to target a database table", () => {
    const result = renderFormScript(`root.append(api.submit("Create contact", { table: "contacts" }));`);

    expect(result.error).toBeUndefined();
    expect(result.elements).toEqual([{ kind: "submit", label: "Create contact", tableName: "contacts" }]);
  });

  it("returns script errors without throwing", () => {
    const result = renderFormScript(`throw new Error("bad form")`);

    expect(result.elements).toEqual([]);
    expect(result.error).toContain("bad form");
  });

  it("supports raw DOM elements appended to root", () => {
    const result = renderFormScript(`
      const note = document.createElement("strong");
      note.textContent = "Custom note";
      root.element.appendChild(note);
    `);

    expect(result.error).toBeUndefined();
    expect(result.elements).toEqual([{ kind: "html", html: "<strong>Custom note</strong>" }]);
  });
});
