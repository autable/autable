import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { FluentProvider, webLightTheme } from "@fluentui/react-components";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { App } from "./App";

beforeEach(() => {
  vi.restoreAllMocks();
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = String(input);
    if (url === "/api/auth/me") {
      return new Response(JSON.stringify({ error: "not authenticated" }), { status: 401 });
    }
    if (url === "/api/auth/oidc/providers") {
      return new Response(JSON.stringify([]), { status: 200 });
    }
    return new Response(JSON.stringify({ error: `unhandled ${url}` }), { status: 404 });
  });
});

function renderApp() {
  return render(
    <FluentProvider theme={webLightTheme}>
      <App />
    </FluentProvider>
  );
}

describe("App", () => {
  it("renders table view first", () => {
    renderApp();
    expect(screen.getByRole("button", { name: "Login" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Register" })).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Table" })).toHaveValue("contacts");
    expect(screen.getByText("3 of 3 records")).toBeInTheDocument();
  });

  it("shows configured OIDC providers as login actions", async () => {
    vi.mocked(fetch).mockImplementation(async (input) => {
      const url = String(input);
      if (url === "/api/auth/me") {
        return new Response(JSON.stringify({ error: "not authenticated" }), { status: 401 });
      }
      if (url === "/api/auth/oidc/providers") {
        return new Response(
          JSON.stringify([{ name: "example", issuer_url: "https://accounts.example.com", scopes: ["openid"] }]),
          { status: 200 }
        );
      }
      return new Response(JSON.stringify({ error: `unhandled ${url}` }), { status: 404 });
    });

    renderApp();
    expect(await screen.findByRole("button", { name: "Continue with example" })).toBeInTheDocument();
  });

  it("shows workflow JavaScript as the workflow view", async () => {
    renderApp();
    await userEvent.click(screen.getByRole("tab", { name: "Workflow" }));
    expect(screen.getByRole("button", { name: "welcome-contact" })).toBeInTheDocument();
    expect((screen.getByLabelText("Workflow JavaScript") as HTMLTextAreaElement).value).toContain(
      'info.node("echo"'
    );
    expect((screen.getByLabelText("Workflow Variables JSON") as HTMLTextAreaElement).value).toContain(
      '"CHANNEL": "ops"'
    );
    expect((screen.getByLabelText("Workflow Secrets JSON") as HTMLTextAreaElement).value).toContain('"TOKEN": ""');
    await userEvent.clear(screen.getByLabelText("Workflow Variables JSON"));
    fireEvent.change(screen.getByLabelText("Workflow Variables JSON"), { target: { value: '{"CHANNEL":"support"}' } });
    expect((screen.getByLabelText("Workflow Variables JSON") as HTMLTextAreaElement).value).toContain("support");
    expect(screen.getByText("echo")).toBeInTheDocument();
    expect(screen.getByText("table.record.changed")).toBeInTheDocument();
    expect(screen.getByText(/history_key:string/)).toBeInTheDocument();
    expect(screen.getByText("No runs yet")).toBeInTheDocument();
  });

  it("shows form JavaScript and preview controls", async () => {
    renderApp();
    await userEvent.click(screen.getByRole("tab", { name: "Form" }));
    expect(screen.getByRole("button", { name: "quick-status" })).toBeInTheDocument();
    expect((screen.getByLabelText("Form JavaScript") as HTMLTextAreaElement).value).toContain("root.append");
    await userEvent.type(screen.getByRole("textbox", { name: "Name" }), "Margaret Hamilton");
    expect(screen.getByRole("button", { name: "Create record" })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Create record" }));
    await userEvent.click(screen.getByRole("tab", { name: "Table" }));
    await waitFor(() => expect(screen.getByText("4 of 4 records")).toBeInTheDocument());

    await userEvent.click(screen.getByRole("tab", { name: "Form" }));
    await userEvent.click(screen.getByRole("button", { name: "quick-status" }));
    expect(screen.getByRole("button", { name: "Update status" })).toBeInTheDocument();
  });
});
