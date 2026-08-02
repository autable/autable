import type { RowListOptions, RowPage, TableMetadata } from "./api";

export type ImportFieldType = "string" | "int" | "float";

export type ParsedSheet = {
  name: string;
  headers: string[];
  // Headers that appeared more than once; only the first occurrence is kept.
  duplicateHeaders: string[];
  rows: Record<string, unknown>[];
};

export type ColumnTarget =
  | { kind: "existing"; type: ImportFieldType; options?: string[] }
  | { kind: "new"; inferredType: ImportFieldType }
  | { kind: "blocked"; reason: "invalidName" | "reservedName" | "unsupportedFieldType" };

export type ImportColumnPlan = {
  header: string;
  include: boolean;
  type: ImportFieldType;
  options?: string[];
};

export type CellProblem =
  | { kind: "type"; header: string; value: unknown; type: ImportFieldType }
  | { kind: "enum"; header: string; value: unknown }
  | { kind: "missingMatch"; header: string };

export type PreparedRow = {
  // 1-based data row number below the header row, for user-facing messages.
  index: number;
  values: Record<string, unknown>;
  problems: CellProblem[];
};

export type DuplicateStrategy = "update" | "skip";

export type ImportRunSummary = {
  fieldsCreated: string[];
  created: number;
  updated: number;
  unchanged: number;
  skipped: number;
  failed: { index: number; error: string }[];
};

export type ImportApi = {
  createFields(fields: { name: string; type: string }[]): Promise<unknown>;
  createRow(values: Record<string, unknown>): Promise<unknown>;
  upsertRow(matchField: string, values: Record<string, unknown>): Promise<{ operation: string }>;
  listRowsPage(options: RowListOptions): Promise<RowPage>;
};

// SheetJS is loaded on demand so the parser stays out of the main bundle.
export async function parseWorkbook(data: ArrayBuffer | Uint8Array): Promise<ParsedSheet[]> {
  const XLSX = await import("xlsx");
  const workbook = XLSX.read(data, { type: "array", cellDates: true });
  return workbook.SheetNames.flatMap((name) => {
    const sheet = workbook.Sheets[name];
    if (!sheet) {
      return [];
    }
    const grid = XLSX.utils.sheet_to_json<unknown[]>(sheet, { header: 1, raw: true, defval: null });
    const headerRowIndex = grid.findIndex((row) => row.some((cell) => !isEmptyCell(cell)));
    if (headerRowIndex === -1) {
      return [];
    }
    const headers: string[] = [];
    const duplicateHeaders: string[] = [];
    // Column index in the grid for each kept header.
    const headerColumns: number[] = [];
    grid[headerRowIndex].forEach((cell, column) => {
      const header = isEmptyCell(cell) ? "" : String(cell).trim();
      if (!header) {
        return;
      }
      if (headers.includes(header)) {
        if (!duplicateHeaders.includes(header)) {
          duplicateHeaders.push(header);
        }
        return;
      }
      headers.push(header);
      headerColumns.push(column);
    });
    const rows: Record<string, unknown>[] = [];
    for (const gridRow of grid.slice(headerRowIndex + 1)) {
      const row: Record<string, unknown> = {};
      headers.forEach((header, headerIndex) => {
        const cell = gridRow[headerColumns[headerIndex]];
        if (!isEmptyCell(cell)) {
          row[header] = cell;
        }
      });
      if (Object.keys(row).length > 0) {
        rows.push(row);
      }
    }
    return [{ name, headers, duplicateHeaders, rows }];
  });
}

function isEmptyCell(cell: unknown): boolean {
  return cell === null || cell === undefined || (typeof cell === "string" && cell.trim() === "");
}

// Mirrors the backend field name rules in unsafeWorkflowFieldNameReason plus
// the reserved ct_ prefix, so invalid headers are rejected before any request.
export function fieldNameProblem(name: string): "invalidName" | "reservedName" | undefined {
  if (name.trim() === "") {
    return "invalidName";
  }
  if (name.startsWith("ct_")) {
    return "reservedName";
  }
  for (const char of name) {
    const code = char.codePointAt(0) ?? 0;
    if (char === "." || char === ";" || char === "`" || code < 0x20 || code === 0x7f) {
      return "invalidName";
    }
  }
  return undefined;
}

export function inferFieldType(values: unknown[]): ImportFieldType {
  let sawNumber = false;
  let allIntegers = true;
  for (const value of values) {
    if (isEmptyCell(value)) {
      continue;
    }
    if (typeof value !== "number" || !Number.isFinite(value)) {
      return "string";
    }
    sawNumber = true;
    if (!Number.isInteger(value)) {
      allIntegers = false;
    }
  }
  if (!sawNumber) {
    return "string";
  }
  return allIntegers ? "int" : "float";
}

// Resolves what importing a header into the table would target. Soft-deleted
// fields keep their original type because the field API restores them.
export function columnTarget(header: string, table: TableMetadata, columnValues: unknown[]): ColumnTarget {
  const nameProblem = fieldNameProblem(header);
  if (nameProblem) {
    return { kind: "blocked", reason: nameProblem };
  }
  const field = table.fields.find((candidate) => candidate.name === header);
  if (!field) {
    return { kind: "new", inferredType: inferFieldType(columnValues) };
  }
  if (field.type !== "string" && field.type !== "int" && field.type !== "float") {
    return { kind: "blocked", reason: "unsupportedFieldType" };
  }
  if (field.deleted) {
    return { kind: "new", inferredType: field.type };
  }
  return { kind: "existing", type: field.type, options: field.options?.length ? field.options : undefined };
}

export function coerceCellValue(
  value: unknown,
  type: ImportFieldType
): { ok: true; value: unknown } | { ok: false } {
  if (isEmptyCell(value)) {
    return { ok: true, value: null };
  }
  switch (type) {
    case "int": {
      if (value instanceof Date) {
        return { ok: true, value: value.getTime() };
      }
      if (typeof value === "number") {
        return Number.isInteger(value) ? { ok: true, value } : { ok: false };
      }
      if (typeof value === "string" && /^[+-]?\d+$/.test(value.trim())) {
        const parsed = Number(value.trim());
        return Number.isSafeInteger(parsed) ? { ok: true, value: parsed } : { ok: false };
      }
      return { ok: false };
    }
    case "float": {
      if (value instanceof Date) {
        return { ok: true, value: value.getTime() };
      }
      if (typeof value === "number" && Number.isFinite(value)) {
        return { ok: true, value };
      }
      if (typeof value === "string") {
        const parsed = Number(value.trim());
        return value.trim() !== "" && Number.isFinite(parsed) ? { ok: true, value: parsed } : { ok: false };
      }
      return { ok: false };
    }
    default: {
      if (value instanceof Date) {
        return { ok: true, value: formatDateCell(value) };
      }
      if (typeof value === "string") {
        return { ok: true, value: value.trim() };
      }
      return { ok: true, value: String(value) };
    }
  }
}

function formatDateCell(value: Date): string {
  const pad = (part: number) => String(part).padStart(2, "0");
  const date = `${value.getFullYear()}-${pad(value.getMonth() + 1)}-${pad(value.getDate())}`;
  if (value.getHours() === 0 && value.getMinutes() === 0 && value.getSeconds() === 0) {
    return date;
  }
  return `${date} ${pad(value.getHours())}:${pad(value.getMinutes())}:${pad(value.getSeconds())}`;
}

export function prepareRows(
  sheet: ParsedSheet,
  columns: ImportColumnPlan[],
  matchField: string | undefined
): PreparedRow[] {
  const included = columns.filter((column) => column.include);
  return sheet.rows.map((row, rowIndex) => {
    const values: Record<string, unknown> = {};
    const problems: CellProblem[] = [];
    for (const column of included) {
      const cell = row[column.header];
      const coerced = coerceCellValue(cell, column.type);
      if (!coerced.ok) {
        problems.push({ kind: "type", header: column.header, value: cell, type: column.type });
        continue;
      }
      if (coerced.value === null) {
        continue;
      }
      if (column.options && !column.options.includes(String(coerced.value))) {
        problems.push({ kind: "enum", header: column.header, value: cell });
        continue;
      }
      values[column.header] = coerced.value;
    }
    if (matchField && values[matchField] === undefined) {
      problems.push({ kind: "missingMatch", header: matchField });
    }
    return { index: rowIndex + 1, values, problems };
  });
}

const MATCH_VALUE_PAGE_SIZE = 1000;

export async function runImport(options: {
  rows: PreparedRow[];
  newFields: { name: string; type: ImportFieldType }[];
  matchField?: string;
  duplicateStrategy: DuplicateStrategy;
  api: ImportApi;
  onProgress?: (done: number, total: number) => void;
  shouldCancel?: () => boolean;
}): Promise<ImportRunSummary> {
  const { rows, newFields, matchField, duplicateStrategy, api, onProgress, shouldCancel } = options;
  const summary: ImportRunSummary = {
    fieldsCreated: [],
    created: 0,
    updated: 0,
    unchanged: 0,
    skipped: 0,
    failed: []
  };
  if (newFields.length > 0) {
    await api.createFields(newFields);
    summary.fieldsCreated = newFields.map((field) => field.name);
  }
  const skipExisting = matchField && duplicateStrategy === "skip" ? await loadMatchValues(api, matchField) : null;
  // Serial on purpose: parallel upserts on the same match value would race
  // into duplicate creates.
  for (const [position, row] of rows.entries()) {
    if (shouldCancel?.()) {
      break;
    }
    try {
      if (matchField) {
        const matchKey = String(row.values[matchField]);
        if (skipExisting) {
          if (skipExisting.has(matchKey)) {
            summary.skipped += 1;
          } else {
            await api.createRow(row.values);
            skipExisting.add(matchKey);
            summary.created += 1;
          }
        } else {
          const mutation = await api.upsertRow(matchField, row.values);
          if (mutation.operation === "create") {
            summary.created += 1;
          } else if (mutation.operation === "update") {
            summary.updated += 1;
          } else {
            summary.unchanged += 1;
          }
        }
      } else {
        await api.createRow(row.values);
        summary.created += 1;
      }
    } catch (error) {
      summary.failed.push({ index: row.index, error: error instanceof Error ? error.message : String(error) });
    }
    onProgress?.(position + 1, rows.length);
  }
  return summary;
}

async function loadMatchValues(api: ImportApi, matchField: string): Promise<Set<string>> {
  const values = new Set<string>();
  let offset = 0;
  for (;;) {
    const page = await api.listRowsPage({ limit: MATCH_VALUE_PAGE_SIZE, offset });
    for (const row of page.rows) {
      const value = row.values[matchField];
      if (value !== undefined && value !== null && value !== "") {
        values.add(String(value));
      }
    }
    offset += page.rows.length;
    if (page.rows.length === 0 || offset >= page.total) {
      return values;
    }
  }
}
