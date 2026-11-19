import { describe, expect, it } from "vitest";
import {
  buildWorkspacePath,
  compactMembers,
  parseWorkspaceRoute,
  replaceResource,
  replaceRole,
  rowDraftFromRecord
} from "./appState";
import type { RoleDefinition } from "./api";

describe("appState", () => {
  it("replaces resources by id and ignores unsaved resources", () => {
    const existing: Array<{ id?: number; name: string }> = [{ id: 1, name: "one" }];

    expect(replaceResource(existing, { id: 1, name: "updated" })).toEqual([{ id: 1, name: "updated" }]);
    expect(replaceResource(existing, { id: 2, name: "two" })).toEqual([
      { id: 1, name: "one" },
      { id: 2, name: "two" }
    ]);
    expect(replaceResource(existing, { name: "draft" })).toBe(existing);
  });

  it("replaces roles by name", () => {
    const existing: RoleDefinition[] = [
      { database_name: "workspace", name: "editor", subject_id: "role:workspace:editor", grants: [], members: [] }
    ];
    const saved: RoleDefinition = {
      database_name: "workspace",
      name: "editor",
      subject_id: "role:workspace:editor",
      grants: [],
      members: [{ type: "user", id: "u1" }]
    };

    expect(replaceRole(existing, saved)).toEqual([saved]);
    expect(replaceRole([], saved)).toEqual([saved]);
  });

  it("compacts role members", () => {
    expect(compactMembers([
      { type: "user", id: " u2 " },
      { type: "user", id: "" },
      { type: "workflow", id: "1" },
      { type: "user", id: "u2" }
    ])).toEqual([
      { type: "user", id: "u2" },
      { type: "workflow", id: "1" }
    ]);
  });

  it("builds a row draft from visible fields", () => {
    expect(rowDraftFromRecord({ name: "Ada", count: 3 }, ["name", "missing", "count"])).toEqual({
      name: "Ada",
      missing: "",
      count: "3"
    });
  });

  it("parses workspace routes", () => {
    expect(parseWorkspaceRoute("/")).toBeNull();
    expect(parseWorkspaceRoute("/databases/workspace")).toEqual({ databaseName: "workspace", view: "table" });
    expect(parseWorkspaceRoute("/databases/workspace/tables/contacts")).toEqual({
      databaseName: "workspace",
      view: "table",
      tableName: "contacts"
    });
    expect(parseWorkspaceRoute("/databases/workspace/tables/contacts/views/active")).toEqual({
      databaseName: "workspace",
      view: "table",
      tableName: "contacts",
      tableViewName: "active"
    });
    expect(parseWorkspaceRoute("/databases/workspace/workflows/12")).toEqual({
      databaseName: "workspace",
      view: "workflow",
      workflowID: 12,
      workflowTab: "editor",
      workflowRunKey: undefined
    });
    expect(parseWorkspaceRoute("/databases/workspace/workflows/12/history/run%2F1")).toEqual({
      databaseName: "workspace",
      view: "workflow",
      workflowID: 12,
      workflowTab: "history",
      workflowRunKey: "run/1"
    });
    expect(parseWorkspaceRoute("/databases/workspace/apps")).toEqual({
      databaseName: "workspace",
      view: "app",
      appName: undefined
    });
    expect(parseWorkspaceRoute("/databases/workspace/apps/orders")).toEqual({
      databaseName: "workspace",
      view: "app",
      appName: "orders"
    });
    expect(parseWorkspaceRoute("/databases/workspace/permissions/admin")).toEqual({
      databaseName: "workspace",
      view: "permission",
      roleName: "admin"
    });
  });

  it("builds encoded workspace paths", () => {
    expect(buildWorkspacePath(null)).toBe("/");
    expect(buildWorkspacePath({ databaseName: "workspace", view: "table" })).toBe("/databases/workspace");
    expect(buildWorkspacePath({
      databaseName: "sales ops",
      view: "table",
      tableName: "customer/list",
      tableViewName: "needs review"
    })).toBe("/databases/sales%20ops/tables/customer%2Flist/views/needs%20review");
    expect(buildWorkspacePath({ databaseName: "workspace", view: "workflow", workflowID: 3 })).toBe(
      "/databases/workspace/workflows/3"
    );
    expect(buildWorkspacePath({
      databaseName: "workspace",
      view: "workflow",
      workflowID: 3,
      workflowTab: "history",
      workflowRunKey: "run/1"
    })).toBe("/databases/workspace/workflows/3/history/run%2F1");
    expect(buildWorkspacePath({ databaseName: "workspace", view: "app" })).toBe("/databases/workspace/apps");
    expect(buildWorkspacePath({ databaseName: "sales ops", view: "app", appName: "orders" })).toBe(
      "/databases/sales%20ops/apps/orders"
    );
    expect(buildWorkspacePath({ databaseName: "workspace", view: "permission", roleName: "owner/admin" })).toBe(
      "/databases/workspace/permissions/owner%2Fadmin"
    );
  });
});
