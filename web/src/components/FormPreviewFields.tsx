import { useEffect, useMemo, useState } from "react";
import {
  Button,
  Caption1,
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
import { Flash24Regular, FlashOff24Regular, ScanQrCode24Regular } from "@fluentui/react-icons";
import type { Column } from "react-data-grid";
import { useTranslation } from "react-i18next";
import { listRows, uploadFile, type RowRecord, type TableMetadata } from "../api";
import type { FormElement } from "../formRuntime";
import { useBarcodeScanner, type BarcodeScanResult } from "../hooks/useBarcodeScanner";
import { rowRecordToValues, type TableGridRow } from "../tableGrid";
import { RecordDataGrid } from "./RecordDataGrid";

type FormPreviewFieldsProps = {
  databaseName: string;
  elements: FormElement[];
  formTable?: string;
  formValues: Record<string, string>;
  result?: unknown;
  onAction: (actionID: string, valueOverrides?: Record<string, string>) => void | Promise<void>;
  onFormValueChange: (name: string, value: string) => void;
  tables?: TableMetadata[];
};

export function FormPreviewFields({
  databaseName,
  elements,
  formTable,
  formValues,
  result,
  onAction,
  onFormValueChange,
  tables = []
}: FormPreviewFieldsProps) {
  const { t } = useTranslation();
  const [resultOpen, setResultOpen] = useState(false);
  useEffect(() => {
    if (result !== undefined && result !== null) {
      setResultOpen(true);
    }
  }, [result]);
  return (
    <>
      {elements.map((element) => {
        if (element.kind === "input") {
          return element.scanner ? (
            <ScannerInput
              key={element.field}
              element={element}
              onAction={onAction}
              onFormValueChange={onFormValueChange}
              value={formValues[element.field] ?? ""}
            />
          ) : (
            <FormTextInput
              key={element.field}
              element={element}
              onAction={onAction}
              onFormValueChange={onFormValueChange}
              value={formValues[element.field] ?? ""}
            />
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
        if (element.kind === "file") {
          return (
            <FileFormInput
              key={element.field}
              databaseName={databaseName}
              element={element}
              formTable={formTable}
              onChange={(value) => onFormValueChange(element.field, value)}
              value={formValues[element.field] ?? ""}
            />
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
        if (element.kind === "button") {
          return (
            <Button key={element.id} type="button" appearance="primary" onClick={() => void onAction(element.actionID)}>
              {element.label}
            </Button>
          );
        }
        return (
          <Button key={element.id} type="button" appearance="primary" onClick={() => void onAction(element.actionID)}>
            {element.label}
          </Button>
        );
      })}
      <Dialog open={resultOpen} onOpenChange={(_, data) => setResultOpen(data.open)}>
        <DialogSurface className="form-result-dialog" style={{ width: "min(1280px, calc(100vw - 32px))", maxWidth: "none" }}>
          <DialogBody>
            <DialogTitle>{t("form.resultDialogTitle")}</DialogTitle>
            <DialogContent className="form-result-content">
              <FormResultView result={result} tables={tables} />
            </DialogContent>
            <DialogActions>
              <Button type="button" onClick={() => setResultOpen(false)}>{t("common.close")}</Button>
            </DialogActions>
          </DialogBody>
        </DialogSurface>
      </Dialog>
    </>
  );
}

function FormTextInput({
  element,
  onAction,
  onFormValueChange,
  value
}: {
  element: Extract<FormElement, { kind: "input" }>;
  onAction: (actionID: string, valueOverrides?: Record<string, string>) => void | Promise<void>;
  onFormValueChange: (name: string, value: string) => void;
  value: string;
}) {
  const handleTextChange = (nextValue: string) => {
    onFormValueChange(element.field, nextValue);
    if (element.onChangeActionID) {
      void onAction(element.onChangeActionID, { [element.field]: nextValue });
    }
  };

  return (
    <label className="field-stack">
      <span>{element.label}</span>
      <Input type={element.inputType} value={value} onChange={(_, data) => handleTextChange(data.value)} />
    </label>
  );
}

function ScannerInput({
  element,
  onAction,
  onFormValueChange,
  value
}: {
  element: Extract<FormElement, { kind: "input" }>;
  onAction: (actionID: string, valueOverrides?: Record<string, string>) => void | Promise<void>;
  onFormValueChange: (name: string, value: string) => void;
  value: string;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [error, setError] = useState("");
  const [pendingResult, setPendingResult] = useState<BarcodeScanResult | null>(null);
  const confirmBeforeWrite = scannerRequiresConfirmation(element.scanner);

  const commitScannedValue = (nextValue: string) => {
    onFormValueChange(element.field, nextValue);
    if (element.onChangeActionID) {
      void onAction(element.onChangeActionID, { [element.field]: nextValue });
    }
  };

  const { videoRef, torchOn, torchAvailable, toggleTorch, resume } = useBarcodeScanner({
    active: open,
    onResult: (scanResult) => {
      if (!scanResult.value) {
        setError(t("form.scannerEmptyResult"));
        resume();
        return;
      }
      if (confirmBeforeWrite) {
        setPendingResult(scanResult);
        return;
      }
      commitScannedValue(scanResult.value);
      setOpen(false);
    },
    onError: (nextError) => {
      setError(t("form.scannerCameraFailed", { error: scannerErrorMessage(nextError) }));
    }
  });

  useEffect(() => {
    if (open) {
      setError("");
      setPendingResult(null);
    }
  }, [open]);

  const handleTextChange = (nextValue: string) => {
    onFormValueChange(element.field, nextValue);
    if (element.onChangeActionID) {
      void onAction(element.onChangeActionID, { [element.field]: nextValue });
    }
  };

  const rescan = () => {
    setPendingResult(null);
    setError("");
    resume();
  };

  return (
    <div className="field-stack">
      <span>{element.label}</span>
      <div className={element.scanner ? "scanner-input" : undefined}>
        <Input
          aria-label={element.label}
          type={element.inputType}
          value={value}
          onChange={(_, data) => handleTextChange(data.value)}
        />
        {element.scanner && (
          <Button
            type="button"
            icon={<ScanQrCode24Regular />}
            aria-label={t("form.scanField", { label: element.label })}
            onClick={() => setOpen(true)}
          >
            {t("form.scan")}
          </Button>
        )}
      </div>
      {element.scanner && <Caption1>{t("form.scannerInput")}</Caption1>}
      {element.scanner && (
        <Dialog open={open} onOpenChange={(_, data) => setOpen(data.open)}>
          <DialogSurface className="scanner-dialog">
            <DialogBody>
              <DialogTitle>{t("form.scannerDialogTitle", { label: element.label })}</DialogTitle>
              <DialogContent className="scanner-content">
                <Text size={200}>{t("form.scannerDialogHint")}</Text>
                {error && <Text className="form-error">{error}</Text>}
                <div className="scanner-video-frame">
                  <video ref={videoRef} className="scanner-video" muted playsInline autoPlay />
                  {pendingResult?.overlay && (
                    <svg
                      className="scanner-overlay"
                      viewBox={pendingResult.overlay.viewBox}
                      preserveAspectRatio="xMidYMid meet"
                      aria-hidden="true"
                    >
                      <polygon points={pendingResult.overlay.points} />
                    </svg>
                  )}
                  {!pendingResult && <div className="scanner-reticle" aria-hidden="true" />}
                </div>
                {pendingResult && (
                  <div className="scanner-detected-value">
                    <Text size={200} weight="semibold">{t("form.scannerDetectedValue")}</Text>
                    <code>{pendingResult.value}</code>
                  </div>
                )}
              </DialogContent>
              <DialogActions>
                {torchAvailable && (
                  <Button
                    type="button"
                    icon={torchOn ? <FlashOff24Regular /> : <Flash24Regular />}
                    onClick={() => void toggleTorch()}
                  >
                    {torchOn ? t("form.scannerTorchOff", "Torch off") : t("form.scannerTorchOn", "Torch on")}
                  </Button>
                )}
                {pendingResult && (
                  <>
                    <Button type="button" onClick={rescan}>{t("form.scannerRescan")}</Button>
                    <Button
                      type="button"
                      appearance="primary"
                      onClick={() => {
                        commitScannedValue(pendingResult.value);
                        setOpen(false);
                      }}
                    >
                      {t("form.scannerConfirm")}
                    </Button>
                  </>
                )}
                <Button type="button" onClick={() => setOpen(false)}>{t("common.cancel")}</Button>
              </DialogActions>
            </DialogBody>
          </DialogSurface>
        </Dialog>
      )}
    </div>
  );
}

function FileFormInput({
  databaseName,
  element,
  formTable,
  onChange,
  value
}: {
  databaseName: string;
  element: Extract<FormElement, { kind: "file" }>;
  formTable?: string;
  onChange: (value: string) => void;
  value: string;
}) {
  const { t } = useTranslation();
  const [fileName, setFileName] = useState("");
  const [error, setError] = useState("");
  const [uploading, setUploading] = useState(false);

  function pickFile() {
    const input = document.createElement("input");
    input.type = "file";
    input.onchange = async () => {
      const file = input.files?.[0];
      if (!file) {
        return;
      }
      setUploading(true);
      setError("");
      try {
        const record = await uploadFile(file, databaseName, formTable ?? "", 0);
        setFileName(record.name);
        onChange(String(record.id));
      } catch (uploadError) {
        setError(uploadError instanceof Error ? uploadError.message : t("form.fileUploadFailed"));
      } finally {
        setUploading(false);
      }
    };
    input.click();
  }

  return (
    <div className="field-stack">
      <span>{element.label}</span>
      <div className="relation-input">
        <Input readOnly value={fileName || (value ? `#${value}` : "")} placeholder={t("form.noFileSelected")} />
        {value && (
          <Button
            type="button"
            onClick={() => {
              onChange("");
              setFileName("");
            }}
          >
            {t("common.clear")}
          </Button>
        )}
        <Button type="button" appearance="primary" disabled={uploading || !formTable} onClick={pickFile}>
          {uploading ? t("form.fileUploading") : t("form.chooseFile")}
        </Button>
      </div>
      {error && <Text className="form-error">{error}</Text>}
    </div>
  );
}

function scannerErrorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
}

function scannerRequiresConfirmation(scanner: Extract<FormElement, { kind: "input" }>["scanner"]): boolean {
  return typeof scanner === "object" && Boolean(scanner.confirm);
}

function FormResultView({ result, tables }: { result?: unknown; tables: TableMetadata[] }) {
  if (result === undefined || result === null) {
    return null;
  }
  if (typeof result === "string" || typeof result === "number" || typeof result === "boolean") {
    return <Text>{String(result)}</Text>;
  }
  if (Array.isArray(result) && result.every(isRowRecord)) {
    if (result.length === 1) {
      return <RecordDetail row={result[0]} tables={tables} />;
    }
    return <RowsResult rows={result} tables={tables} />;
  }
  if (isRowRecord(result)) {
    return <RecordDetail row={result} tables={tables} />;
  }
  return <pre className="form-result-json">{JSON.stringify(result, null, 2)}</pre>;
}

function RowsResult({ rows, tables }: { rows: RowRecord[]; tables: TableMetadata[] }) {
  if (rows.length === 0) {
    return <Text>No records</Text>;
  }
  const fieldNames = formResultFieldNames(rows, tables);
  const columns: Column<TableGridRow>[] = fieldNames.map((fieldName) => ({
    key: fieldName,
    name: fieldName,
    minWidth: Math.max(128, fieldName.length * 14),
    resizable: true,
    renderCell: ({ row }) => String(row[fieldName] ?? "")
  }));
  return (
    <div className="grid-host form-result-grid">
      <RecordDataGrid columns={columns} rows={rows.map(rowRecordToValues)} rowKeyGetter={(row) => row.ct_record_id} />
    </div>
  );
}

function RecordDetail({ row, tables }: { row: RowRecord; tables: TableMetadata[] }) {
  const fieldNames = formResultFieldNames([row], tables);
  return (
    <div className="form-record-detail">
      {fieldNames.map((fieldName) => (
        <div key={fieldName} className="form-record-detail-row">
          <Text size={200} weight="semibold">{fieldName}</Text>
          <Text>{String(row.values[fieldName] ?? "")}</Text>
        </div>
      ))}
    </div>
  );
}

function formResultFieldNames(rows: RowRecord[], tables: TableMetadata[]): string[] {
  const tableFields = [...new Set(tables.flatMap((table) => table.fields.filter((field) => !field.deleted).map((field) => field.name)))];
  const rowFields = [...new Set(rows.flatMap((row) => Object.keys(row.values)))];
  const ordered = tableFields.filter((field) => rowFields.includes(field));
  return ordered.length > 0 ? ordered : rowFields;
}

function isRowRecord(value: unknown): value is RowRecord {
  return Boolean(
    value &&
      typeof value === "object" &&
      typeof (value as RowRecord).record_id === "number" &&
      (value as RowRecord).values &&
      typeof (value as RowRecord).values === "object"
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
  const displayFieldNames = useMemo(() => relationDisplayFieldNames(relationTable, element.fields), [element.fields, relationTable]);
  const filteredRows = useMemo(() => filterRelationRows(rows, searchQuery, displayFieldNames), [displayFieldNames, rows, searchQuery]);
  const gridRows = useMemo(() => filteredRows.map(rowRecordToValues), [filteredRows]);
  const gridColumns = useMemo(
    () => buildRelationGridColumns(displayFieldNames, value, onChange, setOpen, t),
    [displayFieldNames, onChange, t, value]
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
  const selectedLabel = selectedRow ? relationRowLabel(selectedRow, displayFieldNames) : value ? t("form.selectedRecord", { id: value }) : "";

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

function filterRelationRows(rows: RowRecord[], searchQuery: string, fieldNames: string[]): RowRecord[] {
  const query = searchQuery.trim().toLocaleLowerCase();
  if (!query) {
    return rows;
  }
  return rows.filter((row) =>
    [row.record_id, ...fieldNames, ...fieldNames.map((fieldName) => row.values[fieldName])].some((value) =>
      String(value ?? "").toLocaleLowerCase().includes(query)
    )
  );
}

function buildRelationGridColumns(
  fieldNames: string[],
  value: string,
  onChange: (value: string) => void,
  setOpen: (open: boolean) => void,
  t: ReturnType<typeof useTranslation>["t"]
): Column<TableGridRow>[] {
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

function relationDisplayFieldNames(relationTable: TableMetadata | undefined, configuredFields: string[] | undefined): string[] {
  const metadataFieldNames = relationTable?.fields.filter((field) => !field.deleted).map((field) => field.name) ?? [];
  if (!configuredFields) {
    return metadataFieldNames;
  }
  const availableFields = new Set(metadataFieldNames);
  return configuredFields.filter((fieldName) => availableFields.has(fieldName));
}

function relationRowLabel(row: RowRecord, fieldNames: string[]): string {
  const firstValue = fieldNames
    .map((fieldName) => row.values[fieldName])
    .find((value) => value !== undefined && value !== null && String(value).trim() !== "");
  return firstValue === undefined ? `#${row.record_id}` : String(firstValue);
}
