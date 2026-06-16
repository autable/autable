import { describe, expect, it } from "vitest";
import { buildTableColumns, rowRecordToValues } from "./tableGrid";

describe("tableGrid", () => {
  it("builds columns from user fields without exposing record_id", () => {
    const columns = buildTableColumns([
      { name: "name", type: "text", required: true, deleted: false },
      { name: "email", type: "email", required: false, deleted: false }
    ]);

    expect(columns.map((column) => column.id)).toEqual(["name", "email"]);
    expect(columns.map((column) => column.title)).toEqual(["name *", "email"]);
  });

  it("keeps record_id available in row values for internal row operations", () => {
    expect(rowRecordToValues({ record_id: 7, values: { name: "Ada" } })).toEqual({
      record_id: 7,
      name: "Ada"
    });
  });
});
