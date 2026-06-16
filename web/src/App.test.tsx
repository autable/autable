import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { FluentProvider, webLightTheme } from "@fluentui/react-components";
import { describe, expect, it } from "vitest";
import { App } from "./App";

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
    expect(screen.getByRole("combobox", { name: "Table" })).toHaveValue("contacts");
    expect(screen.getByText("3 records")).toBeInTheDocument();
  });

  it("shows workflow JavaScript as the workflow view", async () => {
    renderApp();
    await userEvent.click(screen.getByRole("tab", { name: "Workflow" }));
    expect((screen.getByLabelText("Workflow JavaScript") as HTMLTextAreaElement).value).toContain(
      "export default async function run"
    );
  });

  it("shows form JavaScript and preview controls", async () => {
    renderApp();
    await userEvent.click(screen.getByRole("tab", { name: "Form" }));
    expect((screen.getByLabelText("Form JavaScript") as HTMLTextAreaElement).value).toContain("root.append");
    expect(screen.getByLabelText("Email")).toBeInTheDocument();
  });
});
