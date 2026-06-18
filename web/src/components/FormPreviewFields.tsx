import { useEffect, useMemo, useState, type FormEvent } from "react";
import {
  Button,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  Input,
  Select,
  Text
} from "@fluentui/react-components";
import type { Column } from "react-data-grid";
import { useTranslation } from "react-i18next";
import { listRows, type RowRecord, type TableMetadata } from "../api";
import type { FormElement } from "../formRuntime";
import { rowRecordToValues, type TableGridRow } from "../tableGrid";
import { RecordDataGrid } from "./RecordDataGrid";

type FormPreviewFieldsProps = {
  databaseName: string;
  elements: FormElement[];
  formValues: Record<string, string>;
  onFormValueChange: (name: string, value: string) => void;
  onSubmit: (submitElement?: Extract<FormElement, { kind: "submit" }>, event?: FormEvent<HTMLFormElement>) => void | Promise<void>;
  tables?: TableMetadata[];
};

export function FormPreviewFields({
  databaseName,
  elements,
  formValues,
  onFormValueChange,
  onSubmit,
  tables = []
}: FormPreviewFieldsProps) {
  return (
    <>
      {elements.map((element) => {
        if (element.kind === "input") {
          return (
            <label key={element.field} className="field-stack">
              <span>{element.label}</span>
              <Input
                type={element.inputType}
                value={formValues[element.field] ?? ""}
                onChange={(_, data) => onFormValueChange(element.field, data.value)}
              />
            </label>
          );
        }
        if (element.kind === "select") {
          return (
            <label key={element.field} className="field-stack">
              <span>{element.label}</span>
              <Select
                value={formValues[element.field] ?? element.options[0] ?? ""}
                onChange={(_, data) => onFormValueChange(element.field, data.value)}
              >
                {element.options.map((option) => (
                  <option key={option}>{option}</option>
                ))}
              </Select>
            </label>
          );
        }
        if (element.kind === "relation") {
          const relationTable = tables.find((table) => table.name === element.table);
          return (
            <RelationInput
              key={element.field}
              databaseName={databaseName}
              element={element}
              onChange={(value) => onFormValueChange(element.field, value)}
              relationTable={relationTable}
              value={formValues[element.field] ?? ""}
            />
          );
        }
        if (element.kind === "html") {
          return <div key={element.html} className="form-html" dangerouslySetInnerHTML={{ __html: element.html }} />;
        }
        return (
          <Button key={element.label} type="button" appearance="primary" onClick={() => void onSubmit(element)}>
            {element.label}
          </Button>
        );
      })}
    </>
  );
}

function RelationInput({
  databaseName,
  element,
  onChange,
  relationTable,
  value
}: {
  databaseName: string;
  element: Extract<FormElement, { kind: "relation" }>;
  onChange: (value: string) => void;
  relationTable?: TableMetadata;
  value: string;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [rows, setRows] = useState<RowRecord[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [searchQuery, setSearchQuery] = useState("");
  const filteredRows = useMemo(() => filterRelationRows(rows, searchQuery), [rows, searchQuery]);
  const gridRows = useMemo(() => filteredRows.map(rowRecordToValues), [filteredRows]);
  const gridColumns = useMemo(
    () => buildRelationGridColumns(relationTable, value, onChange, setOpen, t),
    [onChange, relationTable, t, value]
  );

  useEffect(() => {
    let cancelled = false;
    if (!open || !databaseName || !element.table) {
      return () => {
        cancelled = true;
      };
    }
    if (!relationTable) {
      setRows([]);
      setError(t("form.relationMetadataMissing", { table: element.table }));
      return () => {
        cancelled = true;
      };
    }
    setLoading(true);
    setError("");
    void listRows(databaseName, element.table, element.view)
      .then((nextRows) => {
        if (!cancelled) {
          setRows(nextRows);
        }
      })
      .catch((nextError) => {
        if (!cancelled) {
          setRows([]);
          setError(nextError instanceof Error ? nextError.message : t("form.relationLoadFailed"));
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [databaseName, element.table, element.view, open, relationTable, t]);

  useEffect(() => {
    if (!open) {
      setSearchQuery("");
    }
  }, [open]);

  const selectedRow = useMemo(() => rows.find((row) => String(row.record_id) === value), [rows, value]);
  const selectedLabel = selectedRow ? relationRowLabel(selectedRow, relationTable) : value ? t("form.selectedRecord", { id: value }) : "";

  return (
    <div className="field-stack">
      <span>{element.label}</span>
      <div className="relation-input">
        <Input readOnly value={selectedLabel} placeholder={t("form.noRelationSelected")} />
        {value && (
          <Button type="button" onClick={() => onChange("")}>
            {t("common.clear")}
          </Button>
        )}
        <Button type="button" onClick={() => setOpen(true)} disabled={!databaseName || !element.table}>
          {t("form.chooseRelation")}
        </Button>
      </div>
      <Dialog open={open} onOpenChange={(_, data) => setOpen(data.open)}>
        <DialogSurface className="relation-picker-dialog" style={{ width: "min(1280px, calc(100vw - 48px))", maxWidth: "none" }}>
          <DialogBody>
            <DialogTitle>{t("form.relationDialogTitle", { table: element.table })}</DialogTitle>
            <DialogContent className="relation-picker-content">
              {element.view && <Text size={200}>{t("form.relationView", { view: element.view })}</Text>}
              {loading && <Text>{t("form.loadingRelationRecords")}</Text>}
              {error && <Text className="form-error">{error}</Text>}
              {!loading && !error && rows.length === 0 && <Text>{t("form.noRelationRecords")}</Text>}
              {rows.length > 0 && (
                <Input
                  aria-label={t("form.relationSearch")}
                  className="relation-picker-search"
                  type="search"
                  value={searchQuery}
                  placeholder={t("form.relationSearchPlaceholder")}
                  onChange={(_, data) => setSearchQuery(data.value)}
                />
              )}
              {!loading && !error && rows.length > 0 && filteredRows.length === 0 && <Text>{t("form.noRelationSearchResults")}</Text>}
              {filteredRows.length > 0 && (
                <div className="grid-host relation-picker-grid">
                  <RecordDataGrid
                    aria-label={t("form.relationRecords")}
                    columns={gridColumns}
                    rows={gridRows}
                    rowKeyGetter={(row) => row.ct_record_id}
                    onCellClick={({ row }) => {
                      onChange(String(row.ct_record_id));
                      setOpen(false);
                    }}
                  />
                </div>
              )}
            </DialogContent>
            <DialogActions>
              <Button type="button" onClick={() => setOpen(false)}>{t("common.cancel")}</Button>
            </DialogActions>
          </DialogBody>
        </DialogSurface>
      </Dialog>
    </div>
  );
}

function filterRelationRows(rows: RowRecord[], searchQuery: string): RowRecord[] {
  const query = searchQuery.trim().toLocaleLowerCase();
  if (!query) {
    return rows;
  }
  return rows.filter((row) =>
    [row.record_id, ...Object.keys(row.values), ...Object.values(row.values)].some((value) =>
      String(value ?? "").toLocaleLowerCase().includes(query)
    )
  );
}

function buildRelationGridColumns(
  relationTable: TableMetadata | undefined,
  value: string,
  onChange: (value: string) => void,
  setOpen: (open: boolean) => void,
  t: ReturnType<typeof useTranslation>["t"]
): Column<TableGridRow>[] {
  const metadataFieldNames = relationTable?.fields.filter((field) => !field.deleted).map((field) => field.name) ?? [];
  const fieldNames = metadataFieldNames;
  const selectColumn: Column<TableGridRow> = {
    key: "__select__",
    name: "",
    width: 44,
    minWidth: 44,
    maxWidth: 44,
    frozen: true,
    resizable: false,
    renderCell: ({ row }) => (
      <input
        type="radio"
        aria-label={t("form.selectedRecord", { id: row.ct_record_id })}
        checked={String(row.ct_record_id) === value}
        onChange={() => {
          onChange(String(row.ct_record_id));
          setOpen(false);
        }}
        onClick={(event) => event.stopPropagation()}
      />
    )
  };
  return [
    selectColumn,
    ...fieldNames.map((fieldName) => ({
      key: fieldName,
      name: fieldName,
      minWidth: Math.max(128, fieldName.length * 14),
      resizable: true,
      renderCell: ({ row }) => String(row[fieldName] ?? "")
    } satisfies Column<TableGridRow>))
  ];
}

function relationRowLabel(row: RowRecord, relationTable?: TableMetadata): string {
  const fieldNames = relationTable?.fields.filter((field) => !field.deleted).map((field) => field.name) ?? [];
  const firstValue = fieldNames
    .map((fieldName) => row.values[fieldName])
    .find((value) => value !== undefined && value !== null && String(value).trim() !== "");
  return firstValue === undefined ? `#${row.record_id}` : String(firstValue);
}
