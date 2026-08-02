import { describe, expect, it } from "vitest";
import * as XLSX from "xlsx";
import type { RowListOptions, RowPage, TableMetadata } from "./api";
import {
  coerceCellValue,
  columnTarget,
  fieldNameProblem,
  inferFieldType,
  parseWorkbook,
  prepareRows,
  runImport,
  type ImportApi,
  type ImportColumnPlan,
  type ParsedSheet
} from "./excelImport";

function workbookBuffer(sheets: Record<string, unknown[][]>): ArrayBuffer {
  const workbook = XLSX.utils.book_new();
  for (const [name, rows] of Object.entries(sheets)) {
    XLSX.utils.book_append_sheet(workbook, XLSX.utils.aoa_to_sheet(rows), name);
  }
  return XLSX.write(workbook, { type: "array", bookType: "xlsx" }) as ArrayBuffer;
}

const table: TableMetadata = {
  name: "inventory",
  display_name: "Inventory",
  views: [],
  fields: [
    { name: "条码", type: "string", deleted: false },
    { name: "数量", type: "int", deleted: false },
    { name: "状态", type: "string", deleted: false, options: ["在库", "出库"] },
    { name: "合计", type: "formula", deleted: false },
    { name: "旧列", type: "float", deleted: true }
  ]
};

describe("parseWorkbook", () => {
  it("reads headers and rows, skipping empty cells and rows", async () => {
    const sheets = await parseWorkbook(
      workbookBuffer({
        Sheet1: [
          [" 条码 ", "数量", "", "备注"],
          ["A01", 3, null, "ok"],
          [null, null, null, null],
          ["A02", null, null, ""]
        ]
      })
    );
    expect(sheets).toHaveLength(1);
    expect(sheets[0].headers).toEqual(["条码", "数量", "备注"]);
    expect(sheets[0].rows).toEqual([
      { 条码: "A01", 数量: 3, 备注: "ok" },
      { 条码: "A02" }
    ]);
  });

  it("keeps the first occurrence of duplicate headers", async () => {
    const sheets = await parseWorkbook(
      workbookBuffer({ Sheet1: [["条码", "条码"], ["A01", "A02"]] })
    );
    expect(sheets[0].headers).toEqual(["条码"]);
    expect(sheets[0].duplicateHeaders).toEqual(["条码"]);
    expect(sheets[0].rows).toEqual([{ 条码: "A01" }]);
  });

  it("finds the header row below leading blank rows and drops empty sheets", async () => {
    const sheets = await parseWorkbook(
      workbookBuffer({
        Empty: [[]],
        Data: [
          [null, null],
          ["条码"],
          ["A01"]
        ]
      })
    );
    expect(sheets).toHaveLength(1);
    expect(sheets[0].name).toBe("Data");
    expect(sheets[0].rows).toEqual([{ 条码: "A01" }]);
  });
});

describe("inferFieldType", () => {
  it("infers int for whole numbers, float for decimals, string otherwise", () => {
    expect(inferFieldType([1, 2, null])).toBe("int");
    expect(inferFieldType([1, 2.5])).toBe("float");
    expect(inferFieldType([1, "x"])).toBe("string");
    expect(inferFieldType([])).toBe("string");
    expect(inferFieldType([new Date()])).toBe("string");
  });
});

describe("coerceCellValue", () => {
  it("coerces int cells", () => {
    expect(coerceCellValue(3, "int")).toEqual({ ok: true, value: 3 });
    expect(coerceCellValue(" 42 ", "int")).toEqual({ ok: true, value: 42 });
    expect(coerceCellValue(3.5, "int")).toEqual({ ok: false });
    expect(coerceCellValue("三个", "int")).toEqual({ ok: false });
  });

  it("coerces float cells", () => {
    expect(coerceCellValue("3.5", "float")).toEqual({ ok: true, value: 3.5 });
    expect(coerceCellValue("x", "float")).toEqual({ ok: false });
  });

  it("coerces string cells and formats dates", () => {
    expect(coerceCellValue(" a ", "string")).toEqual({ ok: true, value: "a" });
    expect(coerceCellValue(7, "string")).toEqual({ ok: true, value: "7" });
    expect(coerceCellValue(new Date(2026, 7, 2), "string")).toEqual({ ok: true, value: "2026-08-02" });
    expect(coerceCellValue(new Date(2026, 7, 2, 9, 30, 5), "string")).toEqual({ ok: true, value: "2026-08-02 09:30:05" });
  });

  it("treats empty cells as null", () => {
    expect(coerceCellValue(null, "int")).toEqual({ ok: true, value: null });
    expect(coerceCellValue("  ", "string")).toEqual({ ok: true, value: null });
  });

  it("stores dates as millisecond timestamps in numeric fields", () => {
    const date = new Date(2026, 7, 2);
    expect(coerceCellValue(date, "int")).toEqual({ ok: true, value: date.getTime() });
  });
});

describe("fieldNameProblem", () => {
  it("mirrors the backend field name rules", () => {
    expect(fieldNameProblem("条码")).toBeUndefined();
    expect(fieldNameProblem("")).toBe("invalidName");
    expect(fieldNameProblem("a.b")).toBe("invalidName");
    expect(fieldNameProblem("a;b")).toBe("invalidName");
    expect(fieldNameProblem("a`b")).toBe("invalidName");
    expect(fieldNameProblem("ct_x")).toBe("reservedName");
  });
});

describe("columnTarget", () => {
  it("matches existing fields with their type and enum options", () => {
    expect(columnTarget("数量", table, [])).toEqual({ kind: "existing", type: "int" });
    expect(columnTarget("状态", table, [])).toEqual({ kind: "existing", type: "string", options: ["在库", "出库"] });
  });

  it("blocks formula fields and invalid names", () => {
    expect(columnTarget("合计", table, [])).toEqual({ kind: "blocked", reason: "unsupportedFieldType" });
    expect(columnTarget("ct_x", table, [])).toEqual({ kind: "blocked", reason: "reservedName" });
  });

  it("pins soft-deleted fields to their original type", () => {
    expect(columnTarget("旧列", table, ["x"])).toEqual({ kind: "new", inferredType: "float" });
  });

  it("infers the type for new fields", () => {
    expect(columnTarget("盘点人", table, ["张三"])).toEqual({ kind: "new", inferredType: "string" });
    expect(columnTarget("件数", table, [1, 2])).toEqual({ kind: "new", inferredType: "int" });
  });
});

describe("prepareRows", () => {
  const sheet: ParsedSheet = {
    name: "Sheet1",
    headers: ["条码", "数量", "状态", "内部"],
    duplicateHeaders: [],
    rows: [
      { 条码: "A01", 数量: 3, 状态: "在库", 内部: "x" },
      { 条码: "A02", 数量: "三个", 状态: "丢失" },
      { 数量: 5 }
    ]
  };
  const columns: ImportColumnPlan[] = [
    { header: "条码", include: true, type: "string" },
    { header: "数量", include: true, type: "int" },
    { header: "状态", include: true, type: "string", options: ["在库", "出库"] },
    { header: "内部", include: false, type: "string" }
  ];

  it("coerces included columns and reports problems", () => {
    const rows = prepareRows(sheet, columns, "条码");
    expect(rows[0]).toEqual({ index: 1, values: { 条码: "A01", 数量: 3, 状态: "在库" }, problems: [] });
    expect(rows[1].values).toEqual({ 条码: "A02" });
    expect(rows[1].problems).toEqual([
      { kind: "type", header: "数量", value: "三个", type: "int" },
      { kind: "enum", header: "状态", value: "丢失" }
    ]);
    expect(rows[2].problems).toEqual([{ kind: "missingMatch", header: "条码" }]);
  });

  it("does not require a match value without a match field", () => {
    const rows = prepareRows(sheet, columns, undefined);
    expect(rows[2].problems).toEqual([]);
  });
});

type FakeCall = { kind: string; payload: unknown };

function fakeApi(existingRows: Record<string, unknown>[] = []): { api: ImportApi; calls: FakeCall[] } {
  const calls: FakeCall[] = [];
  const api: ImportApi = {
    createFields: async (fields) => {
      calls.push({ kind: "createFields", payload: fields });
      return {};
    },
    createRow: async (values) => {
      calls.push({ kind: "createRow", payload: values });
      return {};
    },
    upsertRow: async (matchField, values) => {
      calls.push({ kind: "upsertRow", payload: { matchField, values } });
      const match = existingRows.find((row) => String(row[matchField]) === String(values[matchField]));
      return { operation: match ? "update" : "create" };
    },
    listRowsPage: async (options: RowListOptions): Promise<RowPage> => {
      calls.push({ kind: "listRowsPage", payload: options });
      const offset = options.offset ?? 0;
      const rows = existingRows.slice(offset, offset + (options.limit ?? existingRows.length));
      return { rows: rows.map((values, i) => ({ record_id: offset + i + 1, values })), total: existingRows.length };
    }
  };
  return { api, calls };
}

describe("runImport", () => {
  const validRows = [
    { index: 1, values: { 条码: "A01", 数量: 3 }, problems: [] },
    { index: 2, values: { 条码: "A02", 数量: 5 }, problems: [] }
  ];

  it("creates fields once and upserts serially with the update strategy", async () => {
    const { api, calls } = fakeApi([{ 条码: "A02" }]);
    const progress: number[] = [];
    const summary = await runImport({
      rows: validRows,
      newFields: [{ name: "数量", type: "int" }],
      matchField: "条码",
      duplicateStrategy: "update",
      api,
      onProgress: (done) => progress.push(done)
    });
    expect(summary).toEqual({
      fieldsCreated: ["数量"],
      created: 1,
      updated: 1,
      unchanged: 0,
      skipped: 0,
      failed: []
    });
    expect(calls.filter((call) => call.kind === "createFields")).toHaveLength(1);
    expect(progress).toEqual([1, 2]);
  });

  it("prefetches existing match values and skips duplicates with the skip strategy", async () => {
    const { api, calls } = fakeApi([{ 条码: "A01" }]);
    const summary = await runImport({
      rows: [...validRows, { index: 3, values: { 条码: "A02" }, problems: [] }],
      newFields: [],
      matchField: "条码",
      duplicateStrategy: "skip",
      api
    });
    expect(summary.created).toBe(1);
    expect(summary.skipped).toBe(2);
    expect(calls.filter((call) => call.kind === "upsertRow")).toHaveLength(0);
  });

  it("appends every row without a match field", async () => {
    const { api, calls } = fakeApi();
    const summary = await runImport({
      rows: validRows,
      newFields: [],
      duplicateStrategy: "update",
      api
    });
    expect(summary.created).toBe(2);
    expect(calls.filter((call) => call.kind === "createRow")).toHaveLength(2);
  });

  it("records per-row failures and keeps going", async () => {
    const { api } = fakeApi();
    const failing: ImportApi = {
      ...api,
      createRow: async (values) => {
        if (values["条码"] === "A01") {
          throw new Error("boom");
        }
        return {};
      }
    };
    const summary = await runImport({
      rows: validRows,
      newFields: [],
      duplicateStrategy: "update",
      api: failing
    });
    expect(summary.created).toBe(1);
    expect(summary.failed).toEqual([{ index: 1, error: "boom" }]);
  });

  it("stops when cancelled", async () => {
    const { api, calls } = fakeApi();
    const summary = await runImport({
      rows: validRows,
      newFields: [],
      duplicateStrategy: "update",
      api,
      shouldCancel: () => calls.length >= 1
    });
    expect(summary.created).toBe(1);
    expect(calls.filter((call) => call.kind === "createRow")).toHaveLength(1);
  });
});
