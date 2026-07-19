import { createElement } from "react";
import { renderTextEditor, type Column } from "react-data-grid";
import type { Field, RowRecord } from "./api";
import { fieldEditable } from "./fieldPermissions";

export type TableGridRow = Record<string, unknown> & { ct_record_id: number };

export type RelationLabelMap = Record<string, Record<number, string>>;

export type FileLabelMap = Record<number, string>;

export type FileCellOptions = {
  labels: FileLabelMap;
  onUpload: (field: Field, recordID: number) => void;
  onDownload: (fileID: number) => void;
};

export function buildTableColumns(
  fields: Field[],
  relationLabels: RelationLabelMap = {},
  onOpenRelation?: (field: Field, recordID: number) => void,
  fileOptions?: FileCellOptions
): Column<TableGridRow>[] {
  return fields.map((field) => ({
    key: field.name,
    name: field.name,
    minWidth: Math.max(128, field.name.length * 14),
    resizable: true,
    renderEditCell: renderTextEditor,
    editable: (row) =>
      Number.isFinite(row.ct_record_id) &&
      field.type !== "formula" &&
      field.type !== "file" &&
      fieldEditable(field.permission_level),
    renderCell: ({ row }) => {
      if (field.type === "file") {
        const recordID = Number(row.ct_record_id);
        const fileID = Number(row[field.name]);
        const canWrite = Number.isFinite(recordID) && fieldEditable(field.permission_level);
        if (!Number.isFinite(fileID) || fileID <= 0) {
          if (!canWrite || !fileOptions) {
            return "";
          }
          return createElement(
            "button",
            {
              type: "button",
              className: "file-cell-upload",
              onClick: () => fileOptions.onUpload(field, recordID)
            },
            "+"
          );
        }
        const label = fileOptions?.labels[fileID] ?? `#${fileID}`;
        return createElement(
          "span",
          {
            className: "file-cell",
            title: label,
            onClick: () => fileOptions?.onDownload(fileID),
            onDoubleClick: () => {
              if (canWrite) {
                fileOptions?.onUpload(field, recordID);
              }
            }
          },
          label
        );
      }
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

export function displayTableCellValue(
  row: TableGridRow,
  field: Field,
  relationLabels: RelationLabelMap = {},
  fileLabels: FileLabelMap = {}
): string {
  const rawValue = row[field.name];
  if (field.type === "file") {
    const fileID = Number(rawValue);
    if (!Number.isFinite(fileID) || fileID <= 0) {
      return "";
    }
    return fileLabels[fileID] ?? `#${fileID}`;
  }
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
  return { ...row.values, ct_record_id: row.record_id };
}
