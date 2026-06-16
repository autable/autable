import type { GridColumn } from "@glideapps/glide-data-grid";
import type { Field, RowRecord } from "./api";

export function buildTableColumns(fields: Field[]): GridColumn[] {
  return fields.map((field) => ({
    id: field.name,
    title: field.required ? `${field.name} *` : field.name,
    width: Math.max(128, field.name.length * 14)
  }));
}

export function rowRecordToValues(row: RowRecord): Record<string, unknown> {
  return { record_id: row.record_id, ...row.values };
}
