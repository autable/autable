import { describe, expect, it } from "vitest";
import { renderFormScript } from "./formRuntime";

describe("renderFormScript", () => {
  it("executes form JavaScript against the form api and root", () => {
    const result = renderFormScript(`
      function render(api, root) {
        const email = api.input({ name: "email", label: "Email", type: "email" });
        const status = api.select({ name: "status", options: ["Active", "Review"] });
        root.append(email, status, api.submit("Create record"));
        return { table: "contacts", fields: { email: "email", status: "status" } };
      }
    `);

    expect(result.error).toBeUndefined();
    expect(result.table).toBe("contacts");
    expect(result.fields).toEqual({ email: "email", status: "status" });
    expect(result.elements).toEqual([
      { kind: "input", name: "email", label: "Email", inputType: "email" },
      { kind: "select", name: "status", label: "status", options: ["Active", "Review"] },
      { kind: "submit", label: "Create record" }
    ]);
  });

  it("rejects direct scripts without a render function", () => {
    const result = renderFormScript(`root.append(api.submit("Create contact"));`);

    expect(result.elements).toEqual([]);
    expect(result.error).toContain("render is not defined");
  });

  it("supports function forms that return the backend submit mapping", () => {
    const result = renderFormScript(`
      function render(api, root) {
        root.append(api.input({ name: "person_name", label: "Name" }), api.submit("Submit"));
        return { table: "contacts", fields: { person_name: "name" } };
      }
    `);

    expect(result.error).toBeUndefined();
    expect(result.table).toBe("contacts");
    expect(result.fields).toEqual({ person_name: "name" });
    expect(result.elements).toEqual([
      { kind: "input", name: "person_name", label: "Name", inputType: "text" },
      { kind: "submit", label: "Submit" }
    ]);
  });

  it("returns script errors without throwing", () => {
    const result = renderFormScript(`throw new Error("bad form")`);

    expect(result.elements).toEqual([]);
    expect(result.error).toContain("bad form");
  });

  it("supports raw DOM elements appended to root", () => {
    const result = renderFormScript(`
      function render(api, root) {
        const note = document.createElement("strong");
        note.textContent = "Custom note";
        root.element.appendChild(note);
        return { table: "contacts", fields: { name: "name" } };
      }
    `);

    expect(result.error).toBeUndefined();
    expect(result.elements).toEqual([{ kind: "html", html: "<strong>Custom note</strong>" }]);
  });
});
