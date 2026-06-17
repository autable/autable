import { useEffect, useMemo, useState } from "react";
import type { CellSelectArgs, RowsChangeData } from "react-data-grid";
import {
  createRow,
  deleteRow,
  listRowHistory,
  listRows,
  loadMetadata,
  updateRow,
  updateTableMetadata,
  type Catalog,
  type Field,
  type RowChange,
  type TableMetadata,
  type TableView,
  type TableViewFilter,
  type TableViewSort
} from "../api";
import { rowDraftFromRecord } from "../appState";
import { buildTableColumns, rowRecordToValues, type TableGridRow } from "../tableGrid";
import { applyTableView } from "../tableViews";

type UseTableWorkspaceOptions = {
  currentUserID?: string;
  databaseName: string;
  selectedTableView: string;
  table: TableMetadata;
  tables: TableMetadata[];
  onCatalogChanged: (catalog: Catalog, tableName: string, viewName: string) => void;
  onStatus: (message: string) => void;
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
  const [rows, setRows] = useState<TableGridRow[]>([]);
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
  const [newViewFilterField, setNewViewFilterField] = useState("");
  const [newViewFilterOp, setNewViewFilterOp] = useState<TableViewFilter["op"]>("eq");
  const [newViewFilterValue, setNewViewFilterValue] = useState("");
  const [newViewSortField, setNewViewSortField] = useState("");
  const [newViewSortDirection, setNewViewSortDirection] = useState<TableViewSort["direction"]>("asc");
  const [relationRows, setRelationRows] = useState<Record<string, TableGridRow[]>>({});
  const [relationDetail, setRelationDetail] = useState<{
    field: Field;
    table: TableMetadata;
    row: TableGridRow;
  } | null>(null);

  const activeFields = table.fields.filter((field) => !field.deleted);
  const activeFieldNames = useMemo(() => activeFields.map((field) => field.name), [table.fields]);
  const displayedRows = useMemo<TableGridRow[]>(
    () => (rowsViewName === selectedTableView ? rows : applyTableView(rows, table.views ?? [], selectedTableView)),
    [rows, rowsViewName, table.views, selectedTableView]
  );
  const displayedRecordIDs = useMemo(
    () => displayedRows.map((row) => Number(row.record_id)).filter((recordID) => Number.isFinite(recordID)),
    [displayedRows]
  );
  const selectedRow = useMemo(
    () => displayedRows.find((row) => Number(row.record_id) === selectedRecordID) ?? null,
    [displayedRows, selectedRecordID]
  );
  const relationLabels = useMemo(() => {
    const labels: Record<string, Record<number, string>> = {};
    for (const field of activeFields) {
      if (field.type !== "relation" || !field.relation_table) {
        continue;
      }
      const targetTable = tables.find((item) => item.name === field.relation_table);
      const labelField = targetTable?.fields.find((item) => !item.deleted && item.name !== "record_id")?.name;
      labels[field.name] = {};
      for (const row of relationRows[field.relation_table] ?? []) {
        labels[field.name][Number(row.record_id)] = String((labelField ? row[labelField] : row.record_id) ?? "");
      }
    }
    return labels;
  }, [activeFields, relationRows, tables]);
  const columns = useMemo(
    () =>
      buildTableColumns(activeFields, relationLabels, (field, recordID) => {
        const targetTable = tables.find((item) => item.name === field.relation_table);
        const row = relationRows[field.relation_table ?? ""]?.find((item) => Number(item.record_id) === recordID);
        if (targetTable && row) {
          setRelationDetail({ field, table: targetTable, row });
        }
      }),
    [activeFields, relationLabels, relationRows, tables]
  );

  useEffect(() => {
    setSelectedRowDraft(rowDraftFromRecord(selectedRow, activeFieldNames));
  }, [activeFieldNames, selectedRow]);

  useEffect(() => {
    const viewDef = table.views.find((item) => item.name === selectedTableView);
    setNewViewBase(viewDef?.base_view ?? "all");
    setNewViewFilterField(viewDef?.filters[0]?.field ?? "");
    setNewViewFilterOp(viewDef?.filters[0]?.op ?? "eq");
    setNewViewFilterValue(String(viewDef?.filters[0]?.value ?? ""));
    setNewViewSortField(viewDef?.sorts[0]?.field ?? "");
    setNewViewSortDirection(viewDef?.sorts[0]?.direction ?? "asc");
  }, [selectedTableView, table.views]);

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
    let cancelled = false;
    if (!currentUserID || !databaseName || !table.name) {
      resetRows(selectedTableView);
      return () => {
        cancelled = true;
      };
    }
    void listRows(databaseName, table.name, selectedTableView)
      .then((nextRows) => {
        if (cancelled) {
          return;
        }
        setRows(nextRows.map(rowRecordToValues));
        setRowsViewName(selectedTableView);
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [currentUserID, databaseName, table.name, selectedTableView]);

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
    setRowsViewName(nextViewName);
    setRowHistory([]);
    setSelectedRecordID(0);
  }

  function addSubmittedRow(targetTableName: string, row: Record<string, unknown>) {
    if (targetTableName !== table.name) {
      return;
    }
    setRows((current) => [...current, row as TableGridRow]);
    setRowsViewName("local");
    setSelectedRecordID(Number(row.record_id));
    setRowHistory([]);
  }

  function selectGridCell(args: CellSelectArgs<TableGridRow>) {
    const recordID = Number(args.row?.record_id);
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
      field === "record_id" ||
      fieldMeta?.type === "formula" ||
      (fieldMeta?.permission_level ?? 2) < 2
    ) {
      return;
    }
    const recordID = Number(nextRow.record_id);
    const nextValue = nextRow[field] ?? "";
    if (previousRow[field] === nextValue) {
      return;
    }
    setRows((current) =>
      current.map((item) => (Number(item.record_id) === recordID ? { ...item, [field]: nextValue } : item))
    );
    try {
      const saved = await updateRow(databaseName, table.name, recordID, { [field]: nextValue });
      setRows((current) =>
        current.map((item) => (Number(item.record_id) === saved.record_id ? rowRecordToValues(saved) : item))
      );
      setRowsViewName("local");
      setSelectedRecordID(saved.record_id);
      setRowHistory([]);
      onStatus(`Updated record ${saved.record_id}`);
    } catch (error) {
      onStatus(error instanceof Error ? error.message : "Row update failed");
    }
  }

  async function persistTableMetadata(nextTable: TableMetadata, successMessage: string, nextViewName = selectedTableView) {
    if (!databaseName || !table.name) {
      onStatus("Select a table before updating metadata");
      return;
    }
    try {
      await updateTableMetadata(databaseName, table.name, nextTable);
      const nextCatalog = await loadMetadata();
      const nextRows = await listRows(databaseName, nextTable.name, nextViewName);
      onCatalogChanged(nextCatalog, nextTable.name, nextViewName);
      setRows(nextRows.map(rowRecordToValues));
      setRowsViewName(nextViewName);
      setRowHistory([]);
      onStatus(successMessage);
    } catch (error) {
      onStatus(error instanceof Error ? error.message : "Table metadata update failed");
    }
  }

  async function addFieldFromCanvas() {
    const name = newFieldName.trim();
    if (!name) {
      onStatus("Field name is required");
      return;
    }
    if (name === "record_id" || table.fields.some((field) => field.name === name && !field.deleted)) {
      onStatus(`Field ${name} already exists`);
      return;
    }
    const formula = newFieldFormula.trim();
    if (newFieldType === "formula" && !formula) {
      onStatus("Formula is required");
      return;
    }
    if (newFieldType === "relation" && !newRelationTable) {
      onStatus("Relation target table is required");
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
    await persistTableMetadata(nextTable, `Added field ${name}`);
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
    await persistTableMetadata(nextTable, `Deleted field ${fieldName}`);
  }

  async function updateFieldFormulaFromCanvas(fieldName: string, formula: string) {
    const trimmedFormula = formula.trim();
    if (!trimmedFormula) {
      onStatus("Formula is required");
      return;
    }
    const field = table.fields.find((item) => item.name === fieldName);
    if (!field || field.type !== "formula") {
      onStatus(`Field ${fieldName} is not a formula field`);
      return;
    }
    const nextTable = {
      ...table,
      fields: table.fields.map((item) => (item.name === fieldName ? { ...item, formula: trimmedFormula } : item))
    };
    await persistTableMetadata(nextTable, `Updated formula ${fieldName}`);
  }

  function viewFiltersFromDraft(): TableViewFilter[] {
    return newViewFilterField
      ? [
          {
            field: newViewFilterField,
            op: newViewFilterOp,
            value: newViewFilterOp === "not_empty" ? undefined : newViewFilterValue
          }
        ]
      : [];
  }

  function viewSortsFromDraft(): TableViewSort[] {
    return newViewSortField ? [{ field: newViewSortField, direction: newViewSortDirection }] : [];
  }

  async function createDefaultViewFromSidebar() {
    if (!databaseName || !table.name) {
      onStatus("Select a table before adding a view");
      return;
    }
    const existingNames = new Set(["all", ...(table.views ?? []).map((viewDef) => viewDef.name)]);
    let index = (table.views ?? []).length + 1;
    while (existingNames.has(`view_${index}`)) {
      index += 1;
    }
    const name = `view_${index}`;
    const displayName = `View ${index}`;
    const nextView: TableView = {
      name,
      display_name: displayName,
      filters: [],
      sorts: []
    };
    const nextTable = { ...table, views: [...(table.views ?? []), nextView] };
    await persistTableMetadata(nextTable, `Created view ${displayName}`, name);
    setNewViewBase("all");
    setNewViewFilterField("");
    setNewViewFilterOp("eq");
    setNewViewFilterValue("");
    setNewViewSortField("");
    setNewViewSortDirection("asc");
  }

  async function updateSelectedViewFromCanvas() {
    const selectedView = table.views.find((viewDef) => viewDef.name === selectedTableView);
    if (!selectedView) {
      onStatus("All records is the base view");
      return;
    }
    const nextView: TableView = {
      ...selectedView,
      base_view: newViewBase === "all" ? undefined : newViewBase,
      filters: viewFiltersFromDraft(),
      sorts: viewSortsFromDraft()
    };
    const nextTable = {
      ...table,
      views: table.views.map((viewDef) => (viewDef.name === selectedView.name ? nextView : viewDef))
    };
    await persistTableMetadata(nextTable, `Updated view ${selectedView.display_name || selectedView.name}`, selectedView.name);
  }

  async function addDraftRow() {
    if (!databaseName || !table.name) {
      onStatus("Create a table before adding rows");
      return;
    }
    const writableFields = activeFields.filter((field) => field.type !== "formula");
    const values = Object.fromEntries(writableFields.map((field) => [field.name, field.name === "status" ? "Review" : ""]));
    values.name = `New record ${rows.length + 1}`;
    try {
      const saved = await createRow(databaseName, table.name, values);
      setRows((current) => [...current, rowRecordToValues(saved)]);
      setRowsViewName("local");
      setSelectedRecordID(saved.record_id);
      setRowHistory([]);
      onStatus(`Created record ${saved.record_id}`);
    } catch (error) {
      onStatus(error instanceof Error ? error.message : "Row creation failed");
    }
  }

  function updateSelectedRowDraft(fieldName: string, value: string) {
    setSelectedRowDraft((current) => ({ ...current, [fieldName]: value }));
  }

  async function updateSelectedRowFromEditor() {
    if (!selectedRecordID) {
      onStatus("Select a row before saving changes");
      return;
    }
    try {
      const writableFieldNames = new Set(activeFields.filter((field) => field.type !== "formula").map((field) => field.name));
      const values = Object.fromEntries(
        Object.entries(selectedRowDraft).filter(([fieldName]) => writableFieldNames.has(fieldName))
      );
      const saved = await updateRow(databaseName, table.name, selectedRecordID, values);
      setRows((current) =>
      current.map((item) => (Number(item.record_id) === saved.record_id ? rowRecordToValues(saved) : item))
      );
      setRowsViewName("local");
      setSelectedRecordID(saved.record_id);
      setRowHistory([]);
      onStatus(`Updated record ${saved.record_id}`);
    } catch (error) {
      onStatus(error instanceof Error ? error.message : "Row update failed");
    }
  }

  async function deleteSelectedRow(recordID = selectedRecordID) {
    if (!recordID) {
      onStatus("Select a row before deleting");
      return;
    }
    try {
      const deleted = await deleteRow(databaseName, table.name, recordID);
      setRows((current) => current.filter((item) => Number(item.record_id) !== deleted.record_id));
      setRowsViewName("local");
      setSelectedRecordID(0);
      setRowHistory([]);
      onStatus(`Deleted record ${deleted.record_id}`);
    } catch (error) {
      onStatus(error instanceof Error ? error.message : "Row deletion failed");
    }
  }

  async function loadSelectedRowHistory() {
    if (!selectedRecordID) {
      onStatus("Select a row before loading history");
      return;
    }
    try {
      const changes = await listRowHistory(databaseName, table.name, selectedRecordID);
      setRowHistory(changes);
      onStatus(`Loaded ${changes.length} history entries for record ${selectedRecordID}`);
    } catch (error) {
      setRowHistory([]);
      onStatus(error instanceof Error ? error.message : "Row history failed");
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
    newViewFilterField,
    newViewFilterOp,
    newViewFilterValue,
    newViewSortDirection,
    newViewSortField,
    rowHistory,
    relationDetail,
    rows,
    selectedRecordID,
    selectedRowDraft,
    addDraftRow,
    addFieldFromCanvas,
    addSubmittedRow,
    createDefaultViewFromSidebar,
    deleteFieldFromCanvas,
    deleteSelectedRow,
    editGridRows,
    loadSelectedRowHistory,
    resetRows,
    setNewFieldName,
    setNewFieldFormula,
    setNewFieldType,
    setNewRelationTable,
    setNewFormulaValueType,
    setRelationDetail,
    setNewViewBase,
    setNewViewFilterField,
    setNewViewFilterOp,
    setNewViewFilterValue,
    setNewViewSortDirection,
    setNewViewSortField,
    setSelectedRecordID,
    selectGridCell,
    updateSelectedViewFromCanvas,
    updateFieldFormulaFromCanvas,
    updateSelectedRowDraft,
    updateSelectedRowFromEditor
  };
}
