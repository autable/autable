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
    expect(parseWorkspaceRoute("/databases/workspace")).toEqual({ databaseName: "workspace", view: "home" });
    expect(parseWorkspaceRoute("/databases/workspace/tables")).toEqual({ databaseName: "workspace", view: "home" });
    expect(parseWorkspaceRoute("/databases/workspace/admin")).toEqual({ databaseName: "workspace", view: "admin" });
    expect(parseWorkspaceRoute("/databases/workspace/admin/extra")).toBeNull();
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
    expect(parseWorkspaceRoute("/databases/workspace/workflows")).toEqual({ databaseName: "workspace", view: "workflow" });
    expect(parseWorkspaceRoute("/databases/workspace/workflows/order-sync")).toEqual({
      databaseName: "workspace",
      view: "workflow",
      workflowName: "order-sync",
      workflowTab: "overview",
      workflowRunID: undefined
    });
    expect(parseWorkspaceRoute("/databases/workspace/workflows/order-sync/versions")).toEqual({
      databaseName: "workspace",
      view: "workflow",
      workflowName: "order-sync",
      workflowTab: "versions",
      workflowRunID: undefined
    });
    expect(parseWorkspaceRoute("/databases/workspace/workflows/order-sync/runs/42")).toEqual({
      databaseName: "workspace",
      view: "workflow",
      workflowName: "order-sync",
      workflowTab: "runs",
      workflowRunID: 42
    });
    expect(parseWorkspaceRoute("/databases/workspace/workflows/order-sync/editor")).toBeNull();
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
    expect(buildWorkspacePath({ databaseName: "workspace", view: "home" })).toBe("/databases/workspace");
    expect(buildWorkspacePath({ databaseName: "workspace", view: "table" })).toBe("/databases/workspace");
    expect(buildWorkspacePath({ databaseName: "sales ops", view: "admin" })).toBe("/databases/sales%20ops/admin");
    expect(buildWorkspacePath({
      databaseName: "sales ops",
      view: "table",
      tableName: "customer/list",
      tableViewName: "needs review"
    })).toBe("/databases/sales%20ops/tables/customer%2Flist/views/needs%20review");
    expect(buildWorkspacePath({ databaseName: "workspace", view: "workflow" })).toBe("/databases/workspace/workflows");
    expect(buildWorkspacePath({ databaseName: "workspace", view: "workflow", workflowName: "order-sync", workflowTab: "overview" })).toBe(
      "/databases/workspace/workflows/order-sync"
    );
    expect(buildWorkspacePath({ databaseName: "workspace", view: "workflow", workflowName: "order-sync", workflowTab: "config" })).toBe(
      "/databases/workspace/workflows/order-sync/config"
    );
    expect(
      buildWorkspacePath({ databaseName: "workspace", view: "workflow", workflowName: "order-sync", workflowTab: "runs", workflowRunID: 7 })
    ).toBe("/databases/workspace/workflows/order-sync/runs/7");
    expect(buildWorkspacePath({ databaseName: "workspace", view: "app" })).toBe("/databases/workspace/apps");
    expect(buildWorkspacePath({ databaseName: "sales ops", view: "app", appName: "orders" })).toBe(
      "/databases/sales%20ops/apps/orders"
    );
    expect(buildWorkspacePath({ databaseName: "workspace", view: "permission", roleName: "staff/lead" })).toBe(
      "/databases/workspace/permissions/staff%2Flead"
    );
  });

  it("routes the instance-wide libraries inside a database's workspace", () => {
    expect(parseWorkspaceRoute("/databases/workspace/libraries")).toEqual({ databaseName: "workspace", view: "library" });
    expect(parseWorkspaceRoute("/databases/workspace/libraries/http")).toEqual({ databaseName: "workspace", view: "library", libraryName: "http" });
    expect(parseWorkspaceRoute("/databases/workspace/libraries/http/extra")).toBeNull();
    expect(buildWorkspacePath({ databaseName: "workspace", view: "library" })).toBe("/databases/workspace/libraries");
    expect(buildWorkspacePath({ databaseName: "sales ops", view: "library", libraryName: "http" })).toBe("/databases/sales%20ops/libraries/http");
  });
});
