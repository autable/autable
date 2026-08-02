import {
  Button,
  Checkbox,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  ProgressBar,
  Select,
  Text,
  ToolbarButton
} from "@fluentui/react-components";
import { ArrowUploadRegular } from "@fluentui/react-icons";
import { useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { createFields, createRow, listRowsPage, upsertRow, type TableMetadata } from "../api";
import {
  columnTarget,
  parseWorkbook,
  prepareRows,
  runImport,
  type CellProblem,
  type ColumnTarget,
  type DuplicateStrategy,
  type ImportColumnPlan,
  type ImportFieldType,
  type ImportRunSummary,
  type ParsedSheet,
  type PreparedRow
} from "../excelImport";

const PREVIEW_ROW_LIMIT = 20;
const IMPORT_TYPES: ImportFieldType[] = ["string", "int", "float"];

export type ExcelImportPinned = {
  // "" pins "no dedup"; undefined lets the user choose.
  matchField?: string;
  // header -> field type; when present, only these headers are importable.
  fields?: Record<string, string>;
  duplicateStrategy?: DuplicateStrategy;
};

type ExcelImportButtonProps = {
  databaseName: string;
  tableName: string;
  tables: TableMetadata[];
  label?: string;
  pinned?: ExcelImportPinned;
  toolbar?: boolean;
  disabled?: boolean;
  onImported?: (summary: ImportRunSummary) => void | Promise<void>;
};

type ColumnState = {
  header: string;
  target: ColumnTarget;
  include: boolean;
  // Column selection and new-field types are locked when the form author
  // pinned the fields config.
  locked: boolean;
  type: ImportFieldType;
};

type ImportStep = "configure" | "preview" | "running" | "done";

export function ExcelImportButton({
  databaseName,
  tableName,
  tables,
  label,
  pinned,
  toolbar = false,
  disabled = false,
  onImported
}: ExcelImportButtonProps) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [fileName, setFileName] = useState("");
  const [sheets, setSheets] = useState<ParsedSheet[]>([]);
  const [sheetIndex, setSheetIndex] = useState(0);
  const [columns, setColumns] = useState<ColumnState[]>([]);
  const [matchField, setMatchField] = useState("");
  const [strategy, setStrategy] = useState<DuplicateStrategy>("update");
  const [step, setStep] = useState<ImportStep>("configure");
  const [skippedConfigure, setSkippedConfigure] = useState(false);
  const [progress, setProgress] = useState({ done: 0, total: 0 });
  const [summary, setSummary] = useState<ImportRunSummary | null>(null);
  const [error, setError] = useState("");
  const [cancelled, setCancelled] = useState(false);
  const cancelRef = useRef(false);

  const table = tables.find((candidate) => candidate.name === tableName);
  const sheet = sheets[sheetIndex];

  const planColumns = useMemo<ImportColumnPlan[]>(
    () =>
      columns.map((column) => ({
        header: column.header,
        include: column.include,
        type: column.type,
        options: column.target.kind === "existing" ? column.target.options : undefined
      })),
    [columns]
  );
  const preparedRows = useMemo<PreparedRow[]>(
    () => (sheet ? prepareRows(sheet, planColumns, matchField || undefined) : []),
    [sheet, planColumns, matchField]
  );
  const validRows = useMemo(() => preparedRows.filter((row) => row.problems.length === 0), [preparedRows]);
  const includedColumns = columns.filter((column) => column.include);
  const newFields = includedColumns
    .filter((column) => column.target.kind === "new")
    .map((column) => ({ name: column.header, type: column.type }));

  function initializeSheet(nextSheets: ParsedSheet[], nextIndex: number): ColumnState[] {
    const nextSheet = nextSheets[nextIndex];
    if (!nextSheet || !table) {
      return [];
    }
    return nextSheet.headers.map((header) => {
      const values = nextSheet.rows.map((row) => row[header]);
      const target = columnTarget(header, table, values);
      const pinnedType = pinned?.fields?.[header];
      const blocked = target.kind === "blocked";
      const include = blocked ? false : pinned?.fields ? header in pinned.fields : true;
      const type =
        target.kind === "existing"
          ? target.type
          : target.kind === "new"
            ? isImportType(pinnedType)
              ? pinnedType
              : target.inferredType
            : "string";
      return { header, target, include, locked: blocked || Boolean(pinned?.fields), type };
    });
  }

  function pickFile() {
    const input = document.createElement("input");
    input.type = "file";
    input.accept = ".xlsx,.xls,.xlsm,.csv";
    input.onchange = async () => {
      const file = input.files?.[0];
      if (!file) {
        return;
      }
      try {
        const parsed = await parseWorkbook(await file.arrayBuffer());
        setFileName(file.name);
        setSheets(parsed);
        setSheetIndex(0);
        setSummary(null);
        setCancelled(false);
        setProgress({ done: 0, total: 0 });
        setStrategy(pinned?.duplicateStrategy ?? "update");
        const nextColumns = parsed.length > 0 ? initializeSheet(parsed, 0) : [];
        setColumns(nextColumns);
        const pinnedMatch = pinned?.matchField;
        const matchUsable =
          pinnedMatch !== undefined &&
          (pinnedMatch === "" || nextColumns.some((column) => column.include && column.header === pinnedMatch));
        setMatchField(matchUsable ? (pinnedMatch as string) : "");
        const skipConfigure = Boolean(pinned?.fields) && matchUsable && parsed.length === 1;
        setSkippedConfigure(skipConfigure);
        setStep(skipConfigure ? "preview" : "configure");
        setError(parsed.length === 0 ? t("import.noData") : "");
        setOpen(true);
      } catch (parseError) {
        setFileName(file.name);
        setSheets([]);
        setColumns([]);
        setError(parseError instanceof Error ? parseError.message : t("import.parseFailed"));
        setStep("configure");
        setOpen(true);
      }
    };
    input.click();
  }

  function selectSheet(nextIndex: number) {
    setSheetIndex(nextIndex);
    setColumns(initializeSheet(sheets, nextIndex));
  }

  function updateColumn(header: string, patch: Partial<Pick<ColumnState, "include" | "type">>) {
    setColumns((current) => current.map((column) => (column.header === header ? { ...column, ...patch } : column)));
    if (patch.include === false && matchField === header) {
      setMatchField("");
    }
  }

  async function startImport() {
    if (!sheet) {
      return;
    }
    cancelRef.current = false;
    setCancelled(false);
    setError("");
    setProgress({ done: 0, total: validRows.length });
    setStep("running");
    try {
      const result = await runImport({
        rows: validRows,
        newFields,
        matchField: matchField || undefined,
        duplicateStrategy: strategy,
        api: {
          createFields: (fields) =>
            createFields(databaseName, tableName, Object.fromEntries(fields.map((field) => [field.name, field.type]))),
          createRow: (values) => createRow(databaseName, tableName, values),
          upsertRow: (field, values) => upsertRow(databaseName, tableName, field, values),
          listRowsPage: (options) => listRowsPage(databaseName, tableName, options)
        },
        onProgress: (done, total) => setProgress({ done, total }),
        shouldCancel: () => cancelRef.current
      });
      setSummary(result);
      setCancelled(cancelRef.current);
      setStep("done");
      if (result.created > 0 || result.updated > 0 || result.fieldsCreated.length > 0) {
        await onImported?.(result);
      }
    } catch (importError) {
      setError(importError instanceof Error ? importError.message : t("import.importFailed"));
      setStep(skippedConfigure ? "preview" : "configure");
    }
  }

  function close() {
    if (step === "running") {
      cancelRef.current = true;
      return;
    }
    setOpen(false);
  }

  const buttonLabel = label || t("import.import");
  const trigger = toolbar ? (
    <ToolbarButton icon={<ArrowUploadRegular />} disabled={disabled} onClick={pickFile}>
      {buttonLabel}
    </ToolbarButton>
  ) : (
    <Button type="button" icon={<ArrowUploadRegular />} disabled={disabled} onClick={pickFile}>
      {buttonLabel}
    </Button>
  );

  return (
    <>
      {trigger}
      <Dialog open={open} onOpenChange={(_, data) => (data.open ? setOpen(true) : close())}>
        <DialogSurface className="import-dialog" style={{ width: "min(960px, calc(100vw - 32px))", maxWidth: "none" }}>
          <DialogBody>
            <DialogTitle>
              {t("import.dialogTitle", { table: table?.display_name || tableName })}
            </DialogTitle>
            <DialogContent className="import-content">
              {!table && <Text className="form-error">{t("import.tableMissing", { table: tableName })}</Text>}
              {error && <Text className="form-error">{error}</Text>}
              {table && sheet && step === "configure" && (
                <ConfigureStep
                  columns={columns}
                  fileName={fileName}
                  matchField={matchField}
                  matchLocked={pinned?.matchField !== undefined && matchField === pinned.matchField}
                  onMatchFieldChange={setMatchField}
                  onSelectSheet={selectSheet}
                  onStrategyChange={setStrategy}
                  onUpdateColumn={updateColumn}
                  sheet={sheet}
                  sheetIndex={sheetIndex}
                  sheets={sheets}
                  strategy={strategy}
                  strategyLocked={pinned?.duplicateStrategy !== undefined}
                />
              )}
              {table && sheet && step === "preview" && (
                <PreviewStep columns={includedColumns} fileName={fileName} preparedRows={preparedRows} validCount={validRows.length} />
              )}
              {step === "running" && (
                <div className="import-progress">
                  <Text>{t("import.importing", { done: progress.done, total: progress.total })}</Text>
                  <ProgressBar value={progress.total === 0 ? 0 : progress.done / progress.total} max={1} />
                </div>
              )}
              {step === "done" && summary && (
                <SummaryView cancelled={cancelled} invalidRows={preparedRows.filter((row) => row.problems.length > 0)} summary={summary} />
              )}
            </DialogContent>
            <DialogActions>
              {step === "configure" && (
                <>
                  <Button type="button" onClick={close}>{t("common.cancel")}</Button>
                  <Button
                    type="button"
                    appearance="primary"
                    disabled={!table || !sheet || includedColumns.length === 0}
                    onClick={() => setStep("preview")}
                  >
                    {t("import.next")}
                  </Button>
                </>
              )}
              {step === "preview" && (
                <>
                  {!skippedConfigure && (
                    <Button type="button" onClick={() => setStep("configure")}>{t("import.back")}</Button>
                  )}
                  <Button type="button" onClick={close}>{t("common.cancel")}</Button>
                  <Button type="button" appearance="primary" disabled={validRows.length === 0} onClick={() => void startImport()}>
                    {t("import.start", { count: validRows.length })}
                  </Button>
                </>
              )}
              {step === "running" && (
                <Button type="button" onClick={() => (cancelRef.current = true)}>{t("import.cancelImport")}</Button>
              )}
              {step === "done" && (
                <Button type="button" appearance="primary" onClick={() => setOpen(false)}>{t("common.close")}</Button>
              )}
            </DialogActions>
          </DialogBody>
        </DialogSurface>
      </Dialog>
    </>
  );
}

function ConfigureStep({
  columns,
  fileName,
  matchField,
  matchLocked,
  onMatchFieldChange,
  onSelectSheet,
  onStrategyChange,
  onUpdateColumn,
  sheet,
  sheetIndex,
  sheets,
  strategy,
  strategyLocked
}: {
  columns: ColumnState[];
  fileName: string;
  matchField: string;
  matchLocked: boolean;
  onMatchFieldChange: (value: string) => void;
  onSelectSheet: (index: number) => void;
  onStrategyChange: (value: DuplicateStrategy) => void;
  onUpdateColumn: (header: string, patch: Partial<Pick<ColumnState, "include" | "type">>) => void;
  sheet: ParsedSheet;
  sheetIndex: number;
  sheets: ParsedSheet[];
  strategy: DuplicateStrategy;
  strategyLocked: boolean;
}) {
  const { t } = useTranslation();
  const includedHeaders = columns.filter((column) => column.include).map((column) => column.header);
  return (
    <div className="import-configure">
      <div className="import-file-summary">
        <Text size={200}>{t("import.fileSummary", { file: fileName, rows: sheet.rows.length })}</Text>
        {sheets.length > 1 && (
          <label className="field-stack">
            <span>{t("import.sheet")}</span>
            <Select value={String(sheetIndex)} onChange={(_, data) => onSelectSheet(Number(data.value))}>
              {sheets.map((candidate, index) => (
                <option key={candidate.name} value={index}>
                  {candidate.name}
                </option>
              ))}
            </Select>
          </label>
        )}
      </div>
      {sheet.duplicateHeaders.length > 0 && (
        <Text size={200} className="form-error">
          {t("import.duplicateHeaders", { headers: sheet.duplicateHeaders.join(", ") })}
        </Text>
      )}
      <Text weight="semibold">{t("import.columnsTitle")}</Text>
      <div className="import-columns">
        {columns.map((column) => (
          <div key={column.header} className="import-column-row">
            <Checkbox
              checked={column.include}
              disabled={column.locked}
              label={column.header}
              onChange={(_, data) => onUpdateColumn(column.header, { include: Boolean(data.checked) })}
            />
            <span className="import-column-target">
              {column.target.kind === "existing" && (
                <Text size={200}>{t("import.existingField", { type: t(`import.type.${column.target.type}`) })}</Text>
              )}
              {column.target.kind === "new" && (
                <span className="import-new-field">
                  <Text size={200}>{t("import.newField")}</Text>
                  <Select
                    aria-label={t("import.newFieldType", { header: column.header })}
                    disabled={column.locked || !column.include}
                    size="small"
                    value={column.type}
                    onChange={(_, data) => onUpdateColumn(column.header, { type: data.value as ImportFieldType })}
                  >
                    {IMPORT_TYPES.map((type) => (
                      <option key={type} value={type}>
                        {t(`import.type.${type}`)}
                      </option>
                    ))}
                  </Select>
                </span>
              )}
              {column.target.kind === "blocked" && (
                <Text size={200} className="import-blocked">
                  {t(`import.blocked.${column.target.reason}`)}
                </Text>
              )}
            </span>
          </div>
        ))}
      </div>
      <div className="import-dedup">
        <label className="field-stack">
          <span>{t("import.matchField")}</span>
          <Select disabled={matchLocked} value={matchField} onChange={(_, data) => onMatchFieldChange(data.value)}>
            <option value="">{t("import.noDedup")}</option>
            {includedHeaders.map((header) => (
              <option key={header} value={header}>
                {header}
              </option>
            ))}
          </Select>
        </label>
        {matchField && (
          <label className="field-stack">
            <span>{t("import.strategy")}</span>
            <Select
              disabled={strategyLocked}
              value={strategy}
              onChange={(_, data) => onStrategyChange(data.value as DuplicateStrategy)}
            >
              <option value="update">{t("import.strategyUpdate")}</option>
              <option value="skip">{t("import.strategySkip")}</option>
            </Select>
          </label>
        )}
      </div>
    </div>
  );
}

function PreviewStep({
  columns,
  fileName,
  preparedRows,
  validCount
}: {
  columns: ColumnState[];
  fileName: string;
  preparedRows: PreparedRow[];
  validCount: number;
}) {
  const { t } = useTranslation();
  const invalidCount = preparedRows.length - validCount;
  return (
    <div className="import-preview">
      <Text size={200}>
        {t("import.previewSummary", { file: fileName, valid: validCount, invalid: invalidCount })}
      </Text>
      <div className="import-preview-scroll">
        <table className="import-preview-table">
          <thead>
            <tr>
              <th>#</th>
              {columns.map((column) => (
                <th key={column.header}>{column.header}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {preparedRows.slice(0, PREVIEW_ROW_LIMIT).map((row) => (
              <tr key={row.index} className={row.problems.length > 0 ? "import-row-invalid" : undefined}>
                <td>{row.index}</td>
                {columns.map((column) => {
                  const problem = row.problems.find((candidate) => candidate.header === column.header);
                  const value = row.values[column.header];
                  return (
                    <td key={column.header} className={problem ? "import-cell-invalid" : undefined}>
                      {problem ? problemText(problem, t) : value === undefined ? "" : String(value)}
                    </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {preparedRows.length > PREVIEW_ROW_LIMIT && (
        <Text size={200}>{t("import.previewTruncated", { shown: PREVIEW_ROW_LIMIT, total: preparedRows.length })}</Text>
      )}
      {invalidCount > 0 && <Text size={200} className="form-error">{t("import.invalidRowsHint", { count: invalidCount })}</Text>}
    </div>
  );
}

function SummaryView({
  cancelled,
  invalidRows,
  summary
}: {
  cancelled: boolean;
  invalidRows: PreparedRow[];
  summary: ImportRunSummary;
}) {
  const { t } = useTranslation();
  return (
    <div className="import-summary">
      {cancelled && <Text className="form-error">{t("import.cancelledHint")}</Text>}
      {summary.fieldsCreated.length > 0 && (
        <Text>{t("import.summaryFields", { count: summary.fieldsCreated.length, names: summary.fieldsCreated.join(", ") })}</Text>
      )}
      <Text>
        {t("import.summaryRows", {
          created: summary.created,
          updated: summary.updated,
          unchanged: summary.unchanged,
          skipped: summary.skipped
        })}
      </Text>
      {invalidRows.length > 0 && <Text className="form-error">{t("import.summaryInvalid", { count: invalidRows.length })}</Text>}
      {summary.failed.length > 0 && (
        <div className="import-failed">
          <Text className="form-error">{t("import.summaryFailed", { count: summary.failed.length })}</Text>
          <ul>
            {summary.failed.slice(0, 10).map((failure) => (
              <li key={failure.index}>
                <Text size={200}>{t("import.failedRow", { index: failure.index, error: failure.error })}</Text>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

function problemText(problem: CellProblem, t: ReturnType<typeof useTranslation>["t"]): string {
  if (problem.kind === "type") {
    return t("import.problemType", { value: String(problem.value), type: t(`import.type.${problem.type}`) });
  }
  if (problem.kind === "enum") {
    return t("import.problemEnum", { value: String(problem.value) });
  }
  return t("import.problemMissingMatch");
}

function isImportType(value: string | undefined): value is ImportFieldType {
  return value === "string" || value === "int" || value === "float";
}
