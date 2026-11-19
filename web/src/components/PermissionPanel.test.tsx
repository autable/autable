import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { FluentProvider, webLightTheme } from "@fluentui/react-components";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { PermissionPanel, compactRoleGrants } from "./PermissionPanel";
import type { AppDefinition, PermissionGrant, TableMetadata, WorkflowDefinition } from "../api";
import i18n from "../i18n";

describe("compactRoleGrants", () => {
  it("keeps positive set and field grants", () => {
    const grants: PermissionGrant[] = [
      { subject_id: "role:workspace:editor", scope: "field_set", resource: "workspace.contacts", field: "", level: 2 },
      { subject_id: "role:workspace:editor", scope: "field", resource: "workspace.contacts", field: "email", level: 1 }
    ];

    expect(compactRoleGrants(grants)).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ scope: "field_set", resource: "workspace.contacts", field: "", level: 2 }),
        expect.objectContaining({ scope: "field", resource: "workspace.contacts", field: "email", level: 1 })
      ])
    );
    expect(compactRoleGrants(grants)).not.toEqual(
      expect.arrayContaining([expect.objectContaining({ scope: "field", field: "legacy" })])
    );
  });

  it("drops none grants", () => {
    const grants: PermissionGrant[] = [
      { subject_id: "role:workspace:editor", scope: "field_set", resource: "workspace.contacts", field: "", level: 0 },
      { subject_id: "role:workspace:editor", scope: "field", resource: "workspace.contacts", field: "email", level: 0 },
      { subject_id: "role:workspace:editor", scope: "workflow", resource: "1", field: "", level: 0 }
    ];

    expect(compactRoleGrants(grants)).toEqual([]);
  });
});

const apps: AppDefinition[] = [
  {
    id: 7,
    database_name: "workspace",
    name: "orders",
    title: "Orders",
    published_version: 1,
    latest_version: 1,
    created_at: 1,
    updated_at: 1,
    url: "/apps/workspace/orders/"
  },
  {
    id: 8,
    database_name: "workspace",
    name: "stock-check",
    title: "",
    published_version: 0,
    latest_version: 1,
    created_at: 1,
    updated_at: 1,
    url: "/apps/workspace/stock-check/"
  }
];

const tables: TableMetadata[] = [
  {
    name: "contacts",
    display_name: "Contacts",
    fields: [
      { name: "name", type: "string", deleted: false },
      { name: "total", type: "formula", formula: "1", deleted: false }
    ],
    views: [{ name: "all", display_name: "", sorts: [] }]
  }
];

const workflows: WorkflowDefinition[] = [
  { id: 3, database_name: "workspace", name: "nightly-sync", script: "", secrets: {}, variables: {} },
  { id: 4, database_name: "workspace", name: "welcome-contact", script: "", secrets: {}, variables: {} }
];

function renderPanel(grants: PermissionGrant[], onGrantChange = vi.fn()) {
  render(
    <FluentProvider theme={webLightTheme}>
      <PermissionPanel
        apps={apps}
        database={{ name: "workspace", tables }}
        grants={grants}
        members={[]}
        memberOptions={[]}
        memberUsers={[]}
        memberWorkflows={[]}
        newMemberQuery=""
        onAddMember={vi.fn()}
        onAddWorkflowMember={vi.fn()}
        onGrantChange={onGrantChange}
        onMemberRemove={vi.fn()}
        onNewMemberQueryChange={vi.fn()}
        onSave={vi.fn()}
        role={{ database_name: "workspace", name: "staff", subject_id: "role:workspace:staff", grants, members: [] }}
        workflows={workflows}
      />
    </FluentProvider>
  );
  return onGrantChange;
}

describe("PermissionPanel apps tab", () => {
  beforeEach(async () => {
    await i18n.changeLanguage("en-US");
  });

  it("offers an all-apps row and one row per app with none and open levels", async () => {
    renderPanel([{ subject_id: "role:workspace:staff", scope: "app", resource: "7", field: "", level: 1 }]);
    await userEvent.click(screen.getByRole("tab", { name: "Apps" }));

    const all = screen.getByLabelText("All apps permission") as HTMLSelectElement;
    expect(Array.from(all.options).map((option) => option.text)).toEqual(["None", "Open"]);
    expect(
      Array.from((screen.getByLabelText("Orders permission") as HTMLSelectElement).options).map((option) => option.text)
    ).toEqual(["None", "Open"]);
    expect(all.value).toBe("0");
    expect((screen.getByLabelText("Orders permission") as HTMLSelectElement).value).toBe("1");
    // An app without a title is listed by its name.
    expect((screen.getByLabelText("stock-check permission") as HTMLSelectElement).value).toBe("0");
  });

  it("grants apps by id and clears per-app grants when choosing all apps", async () => {
    const onGrantChange = renderPanel([
      { subject_id: "role:workspace:staff", scope: "app", resource: "7", field: "", level: 1 }
    ]);
    await userEvent.click(screen.getByRole("tab", { name: "Apps" }));

    await userEvent.selectOptions(screen.getByLabelText("stock-check permission"), "Open");
    expect(onGrantChange).toHaveBeenLastCalledWith("app", "8", "", 1);

    onGrantChange.mockClear();
    await userEvent.selectOptions(screen.getByLabelText("All apps permission"), "Open");
    expect(onGrantChange).toHaveBeenCalledWith("app", "7", "", 0);
    expect(onGrantChange).toHaveBeenCalledWith("app", "8", "", 0);
    expect(onGrantChange).toHaveBeenLastCalledWith("app_set", "workspace", "", 1);
  });
});

describe("PermissionPanel workflows tab", () => {
  beforeEach(async () => {
    await i18n.changeLanguage("en-US");
  });

  it("offers one none/run row per workflow and no all-workflows row", async () => {
    const onGrantChange = renderPanel([
      { subject_id: "role:workspace:staff", scope: "workflow", resource: "3", field: "", level: 1 }
    ]);
    await userEvent.click(screen.getByRole("tab", { name: "Workflows" }));

    const nightly = screen.getByLabelText("nightly-sync permission") as HTMLSelectElement;
    expect(Array.from(nightly.options).map((option) => option.text)).toEqual(["None", "Run"]);
    expect(nightly.value).toBe("1");
    expect((screen.getByLabelText("welcome-contact permission") as HTMLSelectElement).value).toBe("0");
    expect(screen.queryByLabelText("All workflows permission")).not.toBeInTheDocument();

    await userEvent.selectOptions(screen.getByLabelText("welcome-contact permission"), "Run");
    expect(onGrantChange).toHaveBeenLastCalledWith("workflow", "4", "", 1);
  });
});

describe("PermissionPanel tables tab", () => {
  beforeEach(async () => {
    await i18n.changeLanguage("en-US");
  });

  it("only offers data grants, no schema grants", () => {
    renderPanel([]);

    expect(screen.getByRole("button", { name: "Create" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "name Update" })).toBeInTheDocument();
    expect(screen.queryByText("Schema")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add fields" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Field definitions/ })).not.toBeInTheDocument();
  });
});
