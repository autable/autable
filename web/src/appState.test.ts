import { describe, expect, it } from "vitest";
import { compactMembers, replaceResource, replaceRole, rowDraftFromRecord } from "./appState";
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
      members: ["u1"]
    };

    expect(replaceRole(existing, saved)).toEqual([saved]);
    expect(replaceRole([], saved)).toEqual([saved]);
  });

  it("compacts role members", () => {
    expect(compactMembers([" u2 ", "", "u1", "u2"])).toEqual(["u1", "u2"]);
  });

  it("builds a row draft from visible fields", () => {
    expect(rowDraftFromRecord({ name: "Ada", count: 3 }, ["name", "missing", "count"])).toEqual({
      name: "Ada",
      missing: "",
      count: "3"
    });
  });
});
