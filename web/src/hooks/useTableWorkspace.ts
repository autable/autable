import type { Notify } from "../notifications";
import { useEffect, useMemo, useRef, useState } from "react";
import type { CellSelectArgs, RowsChangeData } from "react-data-grid";
import { useTranslation } from "react-i18next";
import {
  createRow,
  deleteRow,
  fetchFileMetadata,
  fileDownloadURL,
  listRowHistory,
  listRows,
  listRowsPage,
  loadMetadata,
  moveTableFieldPosition,
  updateRow,
  uploadFile,
  updateTableMetadata,
  type Catalog,
  type Field,
  type RowChange,
  type TableMetadata,
  type TableView,
  type TableViewQuery,
  type TableViewQueryRule,
  type TableViewSort
} from "../api";
import { rowDraftFromRecord } from "../appState";
import { fieldCreatable, fieldEditable } from "../fieldPermissions";
import { buildTableColumns, rowRecordToValues, type TableGridRow } from "../tableGrid";

export const ROW_PAGE_SIZE = 200;

type UseTableWorkspaceOptions = {
  currentUserID?: string;
  databaseName: string;
  selectedTableView: string;
  table: TableMetadata;
  tables: TableMetadata[];
  onCatalogChanged: (catalog: Catalog, tableName: string, viewName: string) => void;
  onStatus: Notify;
};

export function useTableWorkspace({
  currentUserID,
  databaseName,
  selectedTableView,
  table,
  tables,
  onCatalogChanged,
  onStatus
}: UseTableWorkspaceOptions) {
  const { t } = useTranslation();
  const [rows, setRows] = useState<TableGridRow[]>([]);
  const [total, setTotal] = useState(0);
  const [searchText, setSearchText] = useState("");
  const [search, setSearch] = useState("");
  const loadingMoreRef = useRef(false);
  // Bumped whenever the base query (table/view/sort/search) reloads so
  // in-flight load-more responses for the previous query get discarded.
  const loadGenerationRef = useRef(0);
  const [rowsViewName, setRowsViewName] = useState("all");
  const [selectedRecordID, setSelectedRecordID] = useState(0);
  const [rowHistory, setRowHistory] = useState<RowChange[]>([]);
  const [selectedRowDraft, setSelectedRowDraft] = useState<Record<string, string>>({});
  const [newFieldName, setNewFieldName] = useState("");
  const [newFieldType, setNewFieldType] = useState("string");
  const [newFormulaValueType, setNewFormulaValueType] = useState("string");
  const [newFieldFormula, setNewFieldFormula] = useState("");
  const [newRelationTable, setNewRelationTable] = useState("");
  const [newViewBase, setNewViewBase] = useState("all");
  const [newViewQuery, setNewViewQuery] = useState<TableViewQuery>(() => emptyViewQuery());
  const [newViewSortField, setNewViewSortField] = useState("");
  const [newViewSortDirection, setNewViewSortDirection] = useState<TableViewSort["direction"]>("asc");
  const [temporarySort, setTemporarySort] = useState<TableViewSort | undefined>(undefined);
  const [relationRows, setRelationRows] = useState<Record<string, TableGridRow[]>>({});
  const [fileLabels, setFileLabels] = useState<Record<number, string>>({});
  const [relationDetail, setRelationDetail] = useState<{
    field: Field;
    table: TableMetadata;
    row: TableGridRow;
  } | null>(null);

  const activeFields = table.fields.filter((field) => !field.deleted);
  const activeFieldNames = useMemo(() => activeFields.map((field) => field.name), [table.fields]);
  const displayedRows = useMemo<TableGridRow[]>(() => rows, [rows, rowsViewName, selectedTableView]);
  const displayedRecordIDs = useMemo(
    () => displayedRows.map((row) => Number(row.ct_record_id)).filter((recordID) => Number.isFinite(recordID)),
    [displayedRows]
  );
  const selectedRow = useMemo(
    () => displayedRows.find((row) => Number(row.ct_record_id) === selectedRecordID) ?? null,
    [displayedRows, selectedRecordID]
  );
  const relationLabels = useMemo(() => {
    const labels: Record<string, Record<number, string>> = {};
    for (const field of activeFields) {
      if (field.type !== "relation" || !field.relation_table) {
        continue;
      }
      const targetTable = tables.find((item) => item.name === field.relation_table);
      const labelField = targetTable?.fields.find((item) => !item.deleted && !item.name.startsWith("ct_"))?.name;
      labels[field.name] = {};
      for (const row of relationRows[field.relation_table] ?? []) {
        labels[field.name][Number(row.ct_record_id)] = String((labelField ? row[labelField] : row.ct_record_id) ?? "");
      }
    }
    return labels;
  }, [activeFields, relationRows, tables]);
  const columns = useMemo(
    () =>
      buildTableColumns(
        activeFields,
        relationLabels,
        (field, recordID) => {
          const targetTable = tables.find((item) => item.name === field.relation_table);
          const row = relationRows[field.relation_table ?? ""]?.find((item) => Number(item.ct_record_id) === recordID);
          if (targetTable && row) {
            setRelationDetail({ field, table: targetTable, row });
          }
        },
        {
          labels: fileLabels,
          onUpload: uploadFileToCell,
          onDownload: (fileID) => window.open(fileDownloadURL(fileID), "_blank")
        }
      ),
    [activeFields, fileLabels, relationLabels, relationRows, tables]
  );

  useEffect(() => {
    let cancelled = false;
    const fileFieldNames = activeFields.filter((field) => field.type === "file").map((field) => field.name);
    if (fileFieldNames.length === 0) {
      return () => {
        cancelled = true;
      };
    }
    const missing = new Set<number>();
    for (const row of displayedRows) {
      for (const fieldName of fileFieldNames) {
        const fileID = Number(row[fieldName]);
        if (Number.isFinite(fileID) && fileID > 0 && fileLabels[fileID] === undefined) {
          missing.add(fileID);
        }
      }
    }
    if (missing.size === 0) {
      return () => {
        cancelled = true;
      };
    }
    void fetchFileMetadata([...missing])
      .then((records) => {
        if (cancelled) {
          return;
        }
        setFileLabels((current) => {
          const next = { ...current };
          for (const id of missing) {
            // Missing metadata (deleted files) keeps the #id fallback and
            // stops this effect from refetching the same ids.
            next[id] = next[id] ?? `#${id}`;
          }
          for (const record of records) {
            next[record.id] = record.name;
          }
          return next;
        });
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [activeFields, displayedRows, fileLabels]);

  function uploadFileToCell(field: Field, recordID: number) {
    const input = document.createElement("input");
    input.type = "file";
    input.onchange = async () => {
      const file = input.files?.[0];
      if (!file) {
        return;
      }
      try {
        const record = await uploadFile(file, databaseName, table.name, recordID);
        setFileLabels((current) => ({ ...current, [record.id]: record.name }));
        const saved = await updateRow(databaseName, table.name, recordID, { [field.name]: record.id });
        setRows((current) =>
          current.map((item) => (Number(item.ct_record_id) === saved.record_id ? rowRecordToValues(saved) : item))
        );
        onStatus(t("status.uploadedFile", { name: record.name }));
      } catch (error) {
        onStatus(error instanceof Error ? error.message : t("status.fileUploadFailed"), "error");
      }
    };
    input.click();
  }

  useEffect(() => {
    setSelectedRowDraft(rowDraftFromRecord(selectedRow, activeFieldNames));
  }, [activeFieldNames, selectedRow]);

  useEffect(() => {
    const viewDef = table.views.find((item) => item.name === selectedTableView);
    setNewViewBase(viewDef?.base_view ?? "all");
    setNewViewQuery(normalizeViewQuery(viewDef?.query));
    setNewViewSortField(viewDef?.sorts[0]?.field ?? "");
    setNewViewSortDirection(viewDef?.sorts[0]?.direction ?? "asc");
  }, [selectedTableView, table.views]);

  useEffect(() => {
    setTemporarySort(undefined);
  }, [databaseName, table.name, selectedTableView]);

  useEffect(() => {
    if (displayedRecordIDs.length === 0) {
      setSelectedRecordID(0);
      setRowHistory([]);
      return;
    }
    if (!displayedRecordIDs.includes(selectedRecordID)) {
      setSelectedRecordID(displayedRecordIDs[0]);
      setRowHistory([]);
    }
  }, [displayedRecordIDs, selectedRecordID]);

  useEffect(() => {
    const timer = setTimeout(() => setSearch(searchText.trim()), 300);
    return () => clearTimeout(timer);
  }, [searchText]);

  useEffect(() => {
    let cancelled = false;
    loadGenerationRef.current += 1;
    if (!currentUserID || !databaseName || !table.name) {
      resetRows(selectedTableView);
      return () => {
        cancelled = true;
      };
    }
    void listRowsPage(databaseName, table.name, {
      view: selectedTableView,
      sorts: temporarySort ? [temporarySort] : undefined,
      search,
      limit: ROW_PAGE_SIZE,
      offset: 0
    })
      .then((page) => {
        if (cancelled) {
          return;
        }
        setRows(page.rows.map(rowRecordToValues));
        setTotal(page.total);
        setRowsViewName(selectedTableView);
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [currentUserID, databaseName, table.name, selectedTableView, temporarySort, search]);

  async function loadMoreRows() {
    if (loadingMoreRef.current || !currentUserID || !databaseName || !table.name) {
      return;
    }
    if (rows.length >= total) {
      return;
    }
    loadingMoreRef.current = true;
    const generation = loadGenerationRef.current;
    try {
      const page = await listRowsPage(databaseName, table.name, {
        view: selectedTableView,
        sorts: temporarySort ? [temporarySort] : undefined,
        search,
        limit: ROW_PAGE_SIZE,
        offset: rows.length
      });
      if (generation !== loadGenerationRef.current) {
        return;
      }
      setRows((current) => {
        const seen = new Set(current.map((row) => Number(row.ct_record_id)));
        const appended = page.rows.map(rowRecordToValues).filter((row) => !seen.has(Number(row.ct_record_id)));
        return appended.length > 0 ? [...current, ...appended] : current;
      });
      // An empty page means concurrent deletes shrank the table below our
      // offset — stop asking for more.
      setTotal(page.rows.length === 0 ? rows.length : page.total);
    } catch {
      // Keep what is loaded; the next scroll retries.
    } finally {
      loadingMoreRef.current = false;
    }
  }

  useEffect(() => {
    let cancelled = false;
    const targetTables = Array.from(
      new Set(activeFields.filter((field) => field.type === "relation" && field.relation_table).map((field) => field.relation_table as string))
    );
    if (!currentUserID || !databaseName || targetTables.length === 0) {
      setRelationRows({});
      return () => {
        cancelled = true;
      };
    }
    void Promise.all(
      targetTables.map(async (targetTable) => {
        const targetRows = await listRows(databaseName, targetTable, "all");
        return [targetTable, targetRows.map(rowRecordToValues)] as const;
      })
    )
      .then((entries) => {
        if (!cancelled) {
          setRelationRows(Object.fromEntries(entries));
        }
      })
      .catch(() => {
        if (!cancelled) {
          setRelationRows({});
        }
      });
    return () => {
      cancelled = true;
    };
  }, [table.fields, currentUserID, databaseName]);

  function resetRows(nextViewName = "all") {
    setRows([]);
    setTotal(0);
    setSearchText("");
    setSearch("");
    setRowsViewName(nextViewName);
    setRowHistory([]);
    setSelectedRecordID(0);
  }

  function addSubmittedRow(targetTableName: string, row: Record<string, unknown>) {
    if (targetTableName !== table.name) {
      return;
    }
    setRows((current) => [...current, row as TableGridRow]);
    setTotal((current) => current + 1);
    setRowsViewName("local");
    setSelectedRecordID(Number(row.ct_record_id));
    setRowHistory([]);
  }

  function selectGridCell(args: CellSelectArgs<TableGridRow>) {
    const recordID = Number(args.row?.ct_record_id);
    if (Number.isFinite(recordID)) {
      setSelectedRecordID(recordID);
      setRowHistory([]);
    }
  }

  async function editGridRows(nextRows: TableGridRow[], data: RowsChangeData<TableGridRow>) {
    const rowIndex = data.indexes[0];
    const nextRow = nextRows[rowIndex];
    const previousRow = displayedRows[rowIndex];
    const field = data.column.key;
    const fieldMeta = activeFields.find((item) => item.name === field);
    if (
      !nextRow ||
      !previousRow ||
      field === "ct_record_id" ||
      fieldMeta?.type === "formula" ||
      !fieldEditable(fieldMeta?.permission_level)
    ) {
      return;
    }
    const recordID = Number(nextRow.ct_record_id);
    const nextValue = nextRow[field] ?? "";
    if (previousRow[field] === nextValue) {
      return;
    }
    setRows((current) =>
      current.map((item) => (Number(item.ct_record_id) === recordID ? { ...item, [field]: nextValue } : item))
    );
    try {
      const saved = await updateRow(databaseName, table.name, recordID, { [field]: nextValue });
      setRows((current) =>
        current.map((item) => (Number(item.ct_record_id) === saved.record_id ? rowRecordToValues(saved) : item))
      );
      setRowsViewName("local");
      setSelectedRecordID(saved.record_id);
      setRowHistory([]);
      onStatus(t("status.updatedRecord", { id: saved.record_id }));
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.rowUpdateFailed"), "error");
    }
  }

  async function moveFieldPosition(sourceFieldName: string, targetFieldName: string) {
    if (!databaseName || !table.name) {
      onStatus(t("status.selectTableBeforeMetadata"));
      return;
    }
    if (sourceFieldName === targetFieldName || sourceFieldName === "__add_field__" || targetFieldName === "__add_field__") {
      return;
    }
    if (!activeFields.some((field) => field.name === sourceFieldName) || !activeFields.some((field) => field.name === targetFieldName)) {
      onStatus(t("status.fieldReorderInvalid"));
      return;
    }
    const sourceIndex = activeFields.findIndex((field) => field.name === sourceFieldName);
    const targetIndex = activeFields.findIndex((field) => field.name === targetFieldName);
    const request = sourceIndex < targetIndex ? { after: targetFieldName } : { before: targetFieldName };
    try {
      await moveTableFieldPosition(databaseName, table.name, sourceFieldName, request);
      const nextCatalog = await loadMetadata();
      onCatalogChanged(nextCatalog, table.name, selectedTableView);
      onStatus(t("status.reorderedField", { name: sourceFieldName }));
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.fieldReorderFailed"), "error");
    }
  }

  async function persistTableMetadata(nextTable: TableMetadata, successMessage: string, nextViewName = selectedTableView) {
    if (!databaseName || !table.name) {
      onStatus(t("status.selectTableBeforeMetadata"));
      return;
    }
    try {
      await updateTableMetadata(databaseName, table.name, nextTable);
      const nextCatalog = await loadMetadata();
      const nextTemporarySort = nextTable.fields.some(
        (field) => !field.deleted && field.name === temporarySort?.field
      )
        ? temporarySort
        : undefined;
      if (temporarySort && !nextTemporarySort) {
        setTemporarySort(undefined);
      }
      loadGenerationRef.current += 1;
      const page = await listRowsPage(databaseName, nextTable.name, {
        view: nextViewName,
        sorts: nextTemporarySort ? [nextTemporarySort] : undefined,
        search,
        limit: ROW_PAGE_SIZE,
        offset: 0
      });
      onCatalogChanged(nextCatalog, nextTable.name, nextViewName);
      setRows(page.rows.map(rowRecordToValues));
      setTotal(page.total);
      setRowsViewName(nextViewName);
      setRowHistory([]);
      onStatus(successMessage);
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.tableMetadataUpdateFailed"), "error");
    }
  }

  async function addFieldFromCanvas() {
    const name = newFieldName.trim();
    if (!name) {
      onStatus(t("status.fieldNameRequired"));
      return;
    }
    if (name.startsWith("ct_") || table.fields.some((field) => field.name === name && !field.deleted)) {
      onStatus(t("status.fieldAlreadyExists", { name }));
      return;
    }
    const formula = newFieldFormula.trim();
    if (newFieldType === "formula" && !formula) {
      onStatus(t("status.formulaRequired"));
      return;
    }
    if (newFieldType === "relation" && !newRelationTable) {
      onStatus(t("status.relationTargetRequired"));
      return;
    }
    const nextTable = {
      ...table,
      fields: [
        ...table.fields,
        {
          name,
          type: newFieldType,
          value_type: newFieldType === "formula" ? newFormulaValueType : undefined,
          formula: newFieldType === "formula" ? formula : undefined,
          relation_table: newFieldType === "relation" ? newRelationTable : undefined,
          deleted: false
        }
      ]
    };
    await persistTableMetadata(nextTable, t("status.createdField", { name }));
    setNewFieldName("");
    setNewFieldType("string");
    setNewFormulaValueType("string");
    setNewFieldFormula("");
    setNewRelationTable("");
  }

  async function deleteFieldFromCanvas(fieldName: string) {
    const nextTable = {
      ...table,
      fields: table.fields.map((field) => (field.name === fieldName ? { ...field, deleted: true } : field))
    };
    await persistTableMetadata(nextTable, t("status.deletedField", { name: fieldName }));
  }

  async function updateFieldFormulaFromCanvas(fieldName: string, formula: string) {
    const trimmedFormula = formula.trim();
    if (!trimmedFormula) {
      onStatus(t("status.formulaRequired"));
      return;
    }
    const field = table.fields.find((item) => item.name === fieldName);
    if (!field || field.type !== "formula") {
      onStatus(t("status.fieldNotFormula", { name: fieldName }));
      return;
    }
    const nextTable = {
      ...table,
      fields: table.fields.map((item) => (item.name === fieldName ? { ...item, formula: trimmedFormula } : item))
    };
    await persistTableMetadata(nextTable, t("status.updatedFormula", { name: fieldName }));
  }

  function viewSortsFromDraft(): TableViewSort[] {
    return newViewSortField ? [{ field: newViewSortField, direction: newViewSortDirection }] : [];
  }

  async function updateSelectedViewFromCanvas() {
    const selectedView = table.views.find((viewDef) => viewDef.name === selectedTableView);
    if (!selectedView || selectedView.name === "all") {
      onStatus(t("status.allRecordsBaseView"));
      return;
    }
    const nextView: TableView = {
      ...selectedView,
      base_view: newViewBase === "all" ? undefined : newViewBase,
      query: viewQueryFromDraft(newViewQuery),
      sorts: viewSortsFromDraft()
    };
    const nextTable = {
      ...table,
      views: table.views.map((viewDef) => (viewDef.name === selectedView.name ? nextView : viewDef))
    };
    await persistTableMetadata(
      nextTable,
      t("status.updatedView", { name: selectedView.display_name || selectedView.name }),
      selectedView.name
    );
  }

  async function addDraftRow() {
    if (!databaseName || !table.name) {
      onStatus(t("status.selectTableBeforeRows"));
      return;
    }
    if (selectedTableView !== "all") {
      onStatus(t("status.createRecordFromAllRecordsOnly"));
      return;
    }
    // Only submit fields the actor can write: the server rejects the whole
    // create when the payload contains any unwritable field.
    const writableFields = activeFields.filter((field) => field.type !== "formula" && fieldCreatable(field.permission_level));
    const values = Object.fromEntries(writableFields.map((field) => [field.name, field.name === "status" ? "Review" : ""]));
    if (writableFields.some((field) => field.name === "name")) {
      values.name = `New record ${total + 1}`;
    }
    try {
      const saved = await createRow(databaseName, table.name, values);
      setRows((current) => [...current, rowRecordToValues(saved)]);
      setTotal((current) => current + 1);
      setRowsViewName("local");
      setSelectedRecordID(saved.record_id);
      setRowHistory([]);
      onStatus(t("status.createdRecord", { id: saved.record_id }));
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.rowCreationFailed"), "error");
    }
  }

  function updateSelectedRowDraft(fieldName: string, value: string) {
    setSelectedRowDraft((current) => ({ ...current, [fieldName]: value }));
  }

  async function updateSelectedRowFromEditor() {
    if (!selectedRecordID) {
      onStatus(t("status.selectRowBeforeSave"));
      return;
    }
    try {
      const writableFieldNames = new Set(
        activeFields
          .filter((field) => field.type !== "formula" && fieldEditable(field.permission_level))
          .map((field) => field.name)
      );
      const values = Object.fromEntries(
        Object.entries(selectedRowDraft).filter(([fieldName]) => writableFieldNames.has(fieldName))
      );
      const saved = await updateRow(databaseName, table.name, selectedRecordID, values);
      setRows((current) =>
        current.map((item) => (Number(item.ct_record_id) === saved.record_id ? rowRecordToValues(saved) : item))
      );
      setRowsViewName("local");
      setSelectedRecordID(saved.record_id);
      setRowHistory([]);
      onStatus(t("status.updatedRecord", { id: saved.record_id }));
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.rowUpdateFailed"), "error");
    }
  }

  async function deleteSelectedRow(recordID = selectedRecordID) {
    if (!recordID) {
      onStatus(t("status.selectRowBeforeDelete"));
      return;
    }
    try {
      const deleted = await deleteRow(databaseName, table.name, recordID);
      setRows((current) => current.filter((item) => Number(item.ct_record_id) !== deleted.record_id));
      setTotal((current) => Math.max(0, current - 1));
      setRowsViewName("local");
      setSelectedRecordID(0);
      setRowHistory([]);
      onStatus(t("status.deletedRecord", { id: deleted.record_id }));
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.rowDeletionFailed"), "error");
    }
  }

  async function loadSelectedRowHistory() {
    if (!selectedRecordID) {
      onStatus(t("status.selectRowBeforeHistory"));
      return;
    }
    try {
      const changes = await listRowHistory(databaseName, table.name, selectedRecordID);
      setRowHistory(changes);
      onStatus(t("status.loadedHistory", { count: changes.length, id: selectedRecordID }));
    } catch (error) {
      setRowHistory([]);
      onStatus(error instanceof Error ? error.message : t("status.rowHistoryFailed"), "error");
    }
  }

  return {
    columns,
    displayedRecordIDs,
    displayedRows,
    newFieldName,
    newFieldFormula,
    newFieldType,
    newRelationTable,
    newFormulaValueType,
    newViewBase,
    newViewQuery,
    newViewSortDirection,
    newViewSortField,
    rowHistory,
    relationDetail,
    rows,
    total,
    searchText,
    selectedRecordID,
    selectedRowDraft,
    temporarySort,
    addDraftRow,
    addFieldFromCanvas,
    addSubmittedRow,
    deleteFieldFromCanvas,
    deleteSelectedRow,
    editGridRows,
    moveFieldPosition,
    loadMoreRows,
    loadSelectedRowHistory,
    resetRows,
    setSearchText,
    setNewFieldName,
    setNewFieldFormula,
    setNewFieldType,
    setNewRelationTable,
    setNewFormulaValueType,
    setRelationDetail,
    setNewViewBase,
    setNewViewQuery,
    setNewViewSortDirection,
    setNewViewSortField,
    setSelectedRecordID,
    setTemporarySort,
    selectGridCell,
    updateSelectedViewFromCanvas,
    updateFieldFormulaFromCanvas,
    updateSelectedRowDraft,
    updateSelectedRowFromEditor
  };
}

function emptyViewQuery(): TableViewQuery {
  return { combinator: "and", rules: [] };
}

function normalizeViewQuery(query?: TableViewQuery): TableViewQuery {
  return query ? (viewQueryFromDraft(query) ?? emptyViewQuery()) : emptyViewQuery();
}

function viewQueryFromDraft(query: TableViewQuery): TableViewQuery | undefined {
  const rules = (query.rules ?? [])
    .map((rule) => sanitizeViewQueryRule(rule))
    .filter((rule): rule is TableViewQueryRule => Boolean(rule));
  if (rules.length === 0 && !query.not) {
    return undefined;
  }
  return {
    combinator: query.combinator === "or" ? "or" : "and",
    rules,
    ...(query.not ? { not: true } : {})
  };
}

function sanitizeViewQueryRule(rule: TableViewQueryRule): TableViewQueryRule | undefined {
  if (rule.combinator || rule.rules) {
    const rules = (rule.rules ?? [])
      .map((child) => sanitizeViewQueryRule(child))
      .filter((child): child is TableViewQueryRule => Boolean(child));
    if (rules.length === 0 && !rule.not) {
      return undefined;
    }
    return {
      combinator: rule.combinator === "or" ? "or" : "and",
      rules,
      ...(rule.not ? { not: true } : {})
    };
  }
  if (!rule.field) {
    return undefined;
  }
  const operator = rule.operator || "=";
  return {
    field: rule.field,
    operator,
    ...(operator === "null" || operator === "notNull" ? {} : { value: rule.value ?? "" })
  };
}
