import { createElement, type ChangeEvent } from "react";
import { renderTextEditor, type Column, type RenderEditCellProps } from "react-data-grid";
import type { Field, RowRecord } from "./api";
import { fieldEditable } from "./fieldPermissions";

// Enum string fields edit through a select instead of free text; the empty
// option clears the cell (empty values stay allowed server-side).
function renderEnumEditor(options: string[]) {
  return function EnumEditor({ row, column, onRowChange }: RenderEditCellProps<TableGridRow>) {
    return createElement(
      "select",
      {
        autoFocus: true,
        className: "rdg-text-editor enum-cell-editor",
        "aria-label": column.name,
        value: String(row[column.key] ?? ""),
        onChange: (event: ChangeEvent<HTMLSelectElement>) => {
          onRowChange({ ...row, [column.key]: event.target.value }, true);
        }
      },
      [
        createElement("option", { key: "", value: "" }, ""),
        ...options.map((option) => createElement("option", { key: option, value: option }, option))
      ]
    );
  };
}

// Mirrors metadata.OptionSeparator on the server.
export const OPTION_SEPARATOR = ",";

export function selectedOptions(value: string): string[] {
  return value === "" ? [] : value.split(OPTION_SEPARATOR);
}

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
  fileOptions?: FileCellOptions
): Column<TableGridRow>[] {
  return fields.map((field) => ({
    key: field.name,
    name: field.name,
    minWidth: Math.max(128, field.name.length * 14),
    resizable: true,
    renderEditCell:
      field.type === "string" && field.options?.length && !field.multiple ? renderEnumEditor(field.options) : renderTextEditor,
    // Relation cells stay out of grid editing: double-click opens the
    // relation detail, and letting react-data-grid also start its raw-id
    // editor made the two race. Multi-valued enums are out for a different
    // reason: a cell-sized control cannot offer a set of choices without
    // fighting the grid. Both are edited through the row panel.
    editable: (row) =>
      Number.isFinite(row.ct_record_id) &&
      field.type !== "formula" &&
      field.type !== "file" &&
      field.type !== "relation" &&
      !(field.type === "string" && field.options?.length && field.multiple) &&
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
      // Opening the relation detail is handled by the grid-level
      // onCellDoubleClick: a span-level handler loses the event when the
      // first click's selection re-render replaces the span mid-double-click.
      return createElement(
        "span",
        { className: "relation-cell" },
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
