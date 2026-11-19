import { FileSpreadsheet, Upload } from "lucide-react";
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
import { Badge, Button, Callout, Checkbox, cn, Dialog, DialogContent, Field, NativeSelect } from "../ui";

const PREVIEW_ROW_LIMIT = 20;
const IMPORT_TYPES: ImportFieldType[] = ["string", "int", "float"];

type ExcelImportButtonProps = {
  databaseName: string;
  tableName: string;
  tables: TableMetadata[];
  disabled?: boolean;
  onImported?: (summary: ImportRunSummary) => void | Promise<void>;
};

type ColumnState = {
  header: string;
  target: ColumnTarget;
  include: boolean;
  // Blocked columns cannot be imported.
  locked: boolean;
  type: ImportFieldType;
};

type ImportStep = "configure" | "preview" | "running" | "done";

export function ExcelImportButton({
  databaseName,
  tableName,
  tables,
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
      const blocked = target.kind === "blocked";
      const type = target.kind === "existing" ? target.type : target.kind === "new" ? target.inferredType : "string";
      return { header, target, include: !blocked, locked: blocked, type };
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
        setStrategy("update");
        setColumns(parsed.length > 0 ? initializeSheet(parsed, 0) : []);
        setMatchField("");
        setStep("configure");
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
      setStep("configure");
    }
  }

  function close() {
    if (step === "running") {
      cancelRef.current = true;
      return;
    }
    setOpen(false);
  }

  const stepLabel =
    step === "configure"
      ? t("import.stepConfigure")
      : step === "preview"
        ? t("import.stepPreview")
        : step === "running"
          ? t("import.stepRunning")
          : t("import.stepDone");

  return (
    <>
      <Button variant="quiet" size="sm" className="px-2 sm:px-2.5" aria-label={t("import.import")} disabled={disabled} onClick={pickFile}>
        <Upload aria-hidden />
        <span className="hidden lg:inline">{t("import.import")}</span>
      </Button>
      <Dialog open={open} onOpenChange={(next) => (next ? setOpen(true) : close())}>
        <DialogContent
          title={t("import.dialogTitle", { table: table?.display_name || tableName })}
          description={stepLabel}
          className="max-w-4xl"
          footer={
            <>
              {step === "configure" && (
                <>
                  <Button onClick={close}>{t("common.cancel")}</Button>
                  <Button variant="cta" disabled={!table || !sheet || includedColumns.length === 0} onClick={() => setStep("preview")}>
                    {t("import.next")}
                  </Button>
                </>
              )}
              {step === "preview" && (
                <>
                  <Button onClick={() => setStep("configure")}>{t("import.back")}</Button>
                  <Button onClick={close}>{t("common.cancel")}</Button>
                  <Button variant="cta" disabled={validRows.length === 0} onClick={() => void startImport()}>
                    {t("import.start", { count: validRows.length })}
                  </Button>
                </>
              )}
              {step === "running" && <Button onClick={() => (cancelRef.current = true)}>{t("import.cancelImport")}</Button>}
              {step === "done" && (
                <Button variant="cta" onClick={() => setOpen(false)}>
                  {t("common.close")}
                </Button>
              )}
            </>
          }
        >
          <div className="flex flex-col gap-4 pb-2">
            {!table && <Callout tone="danger">{t("import.tableMissing", { table: tableName })}</Callout>}
            {error && <Callout tone="danger">{error}</Callout>}
            {table && sheet && step === "configure" && (
              <ConfigureStep
                columns={columns}
                fileName={fileName}
                matchField={matchField}
                onMatchFieldChange={setMatchField}
                onSelectSheet={selectSheet}
                onStrategyChange={setStrategy}
                onUpdateColumn={updateColumn}
                sheet={sheet}
                sheetIndex={sheetIndex}
                sheets={sheets}
                strategy={strategy}
              />
            )}
            {table && sheet && step === "preview" && (
              <PreviewStep columns={includedColumns} fileName={fileName} preparedRows={preparedRows} validCount={validRows.length} />
            )}
            {step === "running" && (
              <div className="flex flex-col gap-2 py-6" role="status">
                <p className="text-13 tabular-nums text-description">{t("import.importing", { done: progress.done, total: progress.total })}</p>
                <div
                  className="h-1.5 overflow-hidden rounded-full bg-chip"
                  role="progressbar"
                  aria-valuemin={0}
                  aria-valuemax={progress.total}
                  aria-valuenow={progress.done}
                >
                  <div
                    className="h-full rounded-full bg-fg transition-[width] duration-300"
                    style={{ width: `${progress.total === 0 ? 0 : (progress.done / progress.total) * 100}%` }}
                  />
                </div>
              </div>
            )}
            {step === "done" && summary && (
              <SummaryView cancelled={cancelled} invalidRows={preparedRows.filter((row) => row.problems.length > 0)} summary={summary} />
            )}
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}

function FileSummary({ fileName, rows }: { fileName: string; rows: number }) {
  const { t } = useTranslation();
  return (
    <div className="flex min-w-0 items-center gap-3">
      <span className="flex size-9 shrink-0 items-center justify-center rounded-inner bg-chip text-description" aria-hidden>
        <FileSpreadsheet className="size-4.5" strokeWidth={1.75} />
      </span>
      <span className="min-w-0">
        <span className="block truncate text-13 font-medium text-fg">{fileName}</span>
        <span className="block text-12 text-muted">{t("import.rowCount", { count: rows })}</span>
      </span>
    </div>
  );
}

function ConfigureStep({
  columns,
  fileName,
  matchField,
  onMatchFieldChange,
  onSelectSheet,
  onStrategyChange,
  onUpdateColumn,
  sheet,
  sheetIndex,
  sheets,
  strategy
}: {
  columns: ColumnState[];
  fileName: string;
  matchField: string;
  onMatchFieldChange: (value: string) => void;
  onSelectSheet: (index: number) => void;
  onStrategyChange: (value: DuplicateStrategy) => void;
  onUpdateColumn: (header: string, patch: Partial<Pick<ColumnState, "include" | "type">>) => void;
  sheet: ParsedSheet;
  sheetIndex: number;
  sheets: ParsedSheet[];
  strategy: DuplicateStrategy;
}) {
  const { t } = useTranslation();
  const includedHeaders = columns.filter((column) => column.include).map((column) => column.header);
  return (
    <>
      <div className="flex flex-wrap items-end justify-between gap-3">
        <FileSummary fileName={fileName} rows={sheet.rows.length} />
        {sheets.length > 1 && (
          <Field label={t("import.sheet")} className="w-56">
            {(field) => (
              <NativeSelect {...field} selectSize="sm" value={String(sheetIndex)} onChange={(event) => onSelectSheet(Number(event.target.value))}>
                {sheets.map((candidate, index) => (
                  <option key={candidate.name} value={index}>
                    {candidate.name}
                  </option>
                ))}
              </NativeSelect>
            )}
          </Field>
        )}
      </div>
      {sheet.duplicateHeaders.length > 0 && (
        <Callout tone="warning">{t("import.duplicateHeaders", { headers: sheet.duplicateHeaders.join(", ") })}</Callout>
      )}
      <section className="flex flex-col gap-2">
        <h3 className="text-13 font-medium text-fg">{t("import.columnsTitle")}</h3>
        <ul className="divide-y divide-border-soft rounded-container border border-border">
          {columns.map((column) => (
            <li key={column.header} className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1.5 px-3.5 py-2">
              <Checkbox
                checked={column.include}
                disabled={column.locked}
                label={column.header}
                onChange={(event) => onUpdateColumn(column.header, { include: event.target.checked })}
              />
              <span className="flex items-center gap-2">
                {column.target.kind === "existing" && (
                  <Badge>{t("import.existingField", { type: t(`import.type.${column.target.type}`) })}</Badge>
                )}
                {column.target.kind === "new" && (
                  <>
                    <Badge className="bg-[color-mix(in_oklab,var(--chip),var(--success)_12%)] text-success">{t("import.newField")}</Badge>
                    <NativeSelect
                      selectSize="sm"
                      className="w-32"
                      aria-label={t("import.newFieldType", { header: column.header })}
                      disabled={column.locked || !column.include}
                      value={column.type}
                      onChange={(event) => onUpdateColumn(column.header, { type: event.target.value as ImportFieldType })}
                    >
                      {IMPORT_TYPES.map((type) => (
                        <option key={type} value={type}>
                          {t(`import.type.${type}`)}
                        </option>
                      ))}
                    </NativeSelect>
                  </>
                )}
                {column.target.kind === "blocked" && <span className="text-12 text-warning">{t(`import.blocked.${column.target.reason}`)}</span>}
              </span>
            </li>
          ))}
        </ul>
      </section>
      <section className="grid gap-3 sm:grid-cols-2">
        <Field label={t("import.matchField")}>
          {(field) => (
            <NativeSelect {...field} selectSize="sm" value={matchField} onChange={(event) => onMatchFieldChange(event.target.value)}>
              <option value="">{t("import.noDedup")}</option>
              {includedHeaders.map((header) => (
                <option key={header} value={header}>
                  {header}
                </option>
              ))}
            </NativeSelect>
          )}
        </Field>
        {matchField && (
          <Field label={t("import.strategy")}>
            {(field) => (
              <NativeSelect {...field} selectSize="sm" value={strategy} onChange={(event) => onStrategyChange(event.target.value as DuplicateStrategy)}>
                <option value="update">{t("import.strategyUpdate")}</option>
                <option value="skip">{t("import.strategySkip")}</option>
              </NativeSelect>
            )}
          </Field>
        )}
      </section>
    </>
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
    <>
      <p className="text-13 text-description">{t("import.previewSummary", { file: fileName, valid: validCount, invalid: invalidCount })}</p>
      <div className="max-h-[45vh] overflow-auto rounded-container border border-border">
        <table className="w-full border-collapse text-left text-13" aria-label={t("import.preview")}>
          <thead className="sticky top-0 bg-[color-mix(in_oklab,var(--surface-elevated),var(--text-primary)_2.5%)] text-12 text-muted">
            <tr>
              <th className="h-8 border-b border-border px-3 font-medium">#</th>
              {columns.map((column) => (
                <th key={column.header} className="h-8 whitespace-nowrap border-b border-border px-3 font-medium">
                  {column.header}
                </th>
              ))}
            </tr>
          </thead>
          <tbody className="[&>tr+tr]:border-t [&>tr+tr]:border-border-soft">
            {preparedRows.slice(0, PREVIEW_ROW_LIMIT).map((row) => (
              <tr key={row.index} className={row.problems.length > 0 ? "bg-error-bg/40" : undefined}>
                <td className="h-9 px-3 tabular-nums text-subtle">{row.index}</td>
                {columns.map((column) => {
                  const problem = row.problems.find((candidate) => candidate.header === column.header);
                  const value = row.values[column.header];
                  return (
                    <td key={column.header} className={cn("h-9 whitespace-nowrap px-3", problem ? "text-error" : "text-fg")}>
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
        <p className="text-12 text-muted">{t("import.previewTruncated", { shown: PREVIEW_ROW_LIMIT, total: preparedRows.length })}</p>
      )}
      {invalidCount > 0 && <Callout tone="warning">{t("import.invalidRowsHint", { count: invalidCount })}</Callout>}
    </>
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
    <div className="flex flex-col gap-3">
      {cancelled && <Callout tone="warning">{t("import.cancelledHint")}</Callout>}
      <Callout>
        <p className="text-fg">
          {t("import.summaryRows", {
            created: summary.created,
            updated: summary.updated,
            unchanged: summary.unchanged,
            skipped: summary.skipped
          })}
        </p>
        {summary.fieldsCreated.length > 0 && (
          <p className="mt-1">{t("import.summaryFields", { count: summary.fieldsCreated.length, names: summary.fieldsCreated.join(", ") })}</p>
        )}
      </Callout>
      {invalidRows.length > 0 && <Callout tone="warning">{t("import.summaryInvalid", { count: invalidRows.length })}</Callout>}
      {summary.failed.length > 0 && (
        <Callout tone="danger" title={t("import.summaryFailed", { count: summary.failed.length })}>
          <ul className="mt-1 list-disc pl-4">
            {summary.failed.slice(0, 10).map((failure) => (
              <li key={failure.index}>{t("import.failedRow", { index: failure.index, error: failure.error })}</li>
            ))}
          </ul>
        </Callout>
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
