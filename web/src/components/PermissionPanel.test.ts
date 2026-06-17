import { describe, expect, it } from "vitest";
import { compactRoleGrants } from "./PermissionPanel";
import type { DatabaseMetadata, PermissionGrant } from "../api";

const database: DatabaseMetadata = {
  name: "workspace",
  sqlite_path: "./data/workspace.sqlite",
  tables: [
    {
      name: "contacts",
      display_name: "Contacts",
      fields: [
        { name: "name", type: "string", deleted: false },
        { name: "email", type: "string", deleted: false },
        { name: "legacy", type: "string", deleted: true }
      ],
      views: []
    }
  ]
};

describe("compactRoleGrants", () => {
  it("keeps field none grants when a table grant exists", () => {
    const grants: PermissionGrant[] = [
      { subject_id: "role:workspace:editor", scope: "table", resource: "workspace.contacts", field: "", level: 2 },
      { subject_id: "role:workspace:editor", scope: "field", resource: "workspace.contacts", field: "email", level: 1 }
    ];

    expect(compactRoleGrants(grants, database)).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ scope: "table", resource: "workspace.contacts", field: "", level: 2 }),
        expect.objectContaining({ scope: "field", resource: "workspace.contacts", field: "name", level: 0 }),
        expect.objectContaining({ scope: "field", resource: "workspace.contacts", field: "email", level: 1 })
      ])
    );
    expect(compactRoleGrants(grants, database)).not.toEqual(
      expect.arrayContaining([expect.objectContaining({ scope: "field", field: "legacy" })])
    );
  });

  it("drops none grants that cannot override a table grant", () => {
    const grants: PermissionGrant[] = [
      { subject_id: "role:workspace:editor", scope: "table", resource: "workspace.contacts", field: "", level: 0 },
      { subject_id: "role:workspace:editor", scope: "field", resource: "workspace.contacts", field: "email", level: 0 },
      { subject_id: "role:workspace:editor", scope: "workflow", resource: "1", field: "", level: 0 }
    ];

    expect(compactRoleGrants(grants, database)).toEqual([]);
  });
});
