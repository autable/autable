import { textEditor, type Column } from "react-data-grid";
import type { Field, RowRecord } from "./api";

export type TableGridRow = Record<string, unknown> & { record_id: number };

export function buildTableColumns(fields: Field[]): Column<TableGridRow>[] {
  return fields.map((field) => ({
    key: field.name,
    name: field.required ? `${field.name} *` : field.name,
    minWidth: Math.max(128, field.name.length * 14),
    resizable: true,
    renderEditCell: textEditor,
    editable: (row) => Number.isFinite(row.record_id) && (field.permission_level ?? 2) >= 2,
    renderCell: ({ row }) => String(row[field.name] ?? "")
  }));
}

export function rowRecordToValues(row: RowRecord): TableGridRow {
  return { record_id: row.record_id, ...row.values };
}
