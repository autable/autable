import { FluentProvider, webLightTheme } from "@fluentui/react-components";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { EnumOptionCheckboxes } from "./EnumOptionCheckboxes";

function renderCheckboxes(value: string, onChange = vi.fn()) {
  render(
    <FluentProvider theme={webLightTheme}>
      <EnumOptionCheckboxes label="渠道" options={["邮件", "电话", "邮寄"]} value={value} onChange={onChange} />
    </FluentProvider>
  );
  return onChange;
}

describe("EnumOptionCheckboxes", () => {
  it("checks the options the cell already holds", () => {
    renderCheckboxes("邮件,邮寄");
    expect(screen.getByLabelText("邮件")).toBeChecked();
    expect(screen.getByLabelText("电话")).not.toBeChecked();
    expect(screen.getByLabelText("邮寄")).toBeChecked();
  });

  it("joins a new selection in the declared option order", async () => {
    const user = userEvent.setup();
    // Clicking out of order must still store the canonical value.
    const onChange = renderCheckboxes("邮寄");
    await user.click(screen.getByLabelText("邮件"));
    expect(onChange).toHaveBeenCalledWith("邮件,邮寄");
  });

  it("clears an option without disturbing the rest", async () => {
    const user = userEvent.setup();
    const onChange = renderCheckboxes("邮件,电话,邮寄");
    await user.click(screen.getByLabelText("电话"));
    expect(onChange).toHaveBeenCalledWith("邮件,邮寄");
  });

  it("empties the cell when the last option is cleared", async () => {
    const user = userEvent.setup();
    const onChange = renderCheckboxes("电话");
    await user.click(screen.getByLabelText("电话"));
    expect(onChange).toHaveBeenCalledWith("");
  });
});
