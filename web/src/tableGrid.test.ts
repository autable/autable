import { describe, expect, it } from "vitest";
import { buildTableColumns, rowRecordToValues } from "./tableGrid";

describe("tableGrid", () => {
  it("builds columns from user fields without exposing ct_record_id", () => {
    const columns = buildTableColumns([
      { name: "name", type: "string", deleted: false },
      { name: "email", type: "string", deleted: false }
    ]);

    expect(columns.map((column) => column.key)).toEqual(["name", "email"]);
    expect(columns.map((column) => column.name)).toEqual(["name", "email"]);
  });

  it("does not make formula fields editable", () => {
    const columns = buildTableColumns([
      { name: "score", type: "float", deleted: false },
      { name: "score_plus_one", type: "formula", value_type: "float", formula: `fields["score"] + 1`, deleted: false }
    ]);

    expect(typeof columns[0].editable === "function" ? columns[0].editable({ ct_record_id: 1 }) : columns[0].editable).toBe(true);
    expect(typeof columns[1].editable === "function" ? columns[1].editable({ ct_record_id: 1 }) : columns[1].editable).toBe(false);
  });

  it("keeps ct_record_id available in row values for internal row operations", () => {
    expect(rowRecordToValues({ record_id: 7, values: { name: "Ada" } })).toEqual({
      name: "Ada",
      ct_record_id: 7
    });
  });
});
