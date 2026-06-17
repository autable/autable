import { createElement } from "react";
import { textEditor, type Column } from "react-data-grid";
import type { Field, RowRecord } from "./api";

export type TableGridRow = Record<string, unknown> & { record_id: number };

export type RelationLabelMap = Record<string, Record<number, string>>;

export function buildTableColumns(
  fields: Field[],
  relationLabels: RelationLabelMap = {},
  onOpenRelation?: (field: Field, recordID: number) => void
): Column<TableGridRow>[] {
  return fields.map((field) => ({
    key: field.name,
    name: field.name,
    minWidth: Math.max(128, field.name.length * 14),
    resizable: true,
    renderEditCell: textEditor,
    editable: (row) => Number.isFinite(row.record_id) && field.type !== "formula" && (field.permission_level ?? 2) >= 2,
    renderCell: ({ row }) => {
      if (field.type !== "relation") {
        return String(row[field.name] ?? "");
      }
      const recordID = Number(row[field.name]);
      if (!Number.isFinite(recordID) || recordID <= 0) {
        return "";
      }
      return createElement(
        "span",
        {
          className: "relation-cell",
          onDoubleClick: () => onOpenRelation?.(field, recordID)
        },
        relationLabels[field.name]?.[recordID] || `#${recordID}`
      );
    }
  }));
}

export function displayTableCellValue(row: TableGridRow, field: Field, relationLabels: RelationLabelMap = {}): string {
  const rawValue = row[field.name];
  if (field.type !== "relation") {
    return String(rawValue ?? "");
  }
  const recordID = Number(rawValue);
  if (!Number.isFinite(recordID) || recordID <= 0) {
    return "";
  }
  return relationLabels[field.name]?.[recordID] || `#${recordID}`;
}

export function rowRecordToValues(row: RowRecord): TableGridRow {
  return { record_id: row.record_id, ...row.values };
}
