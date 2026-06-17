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
  onCatalogChanged: (catalog: Catalog, tableName: string, viewName: string) => void;
  onStatus: (message: string) => void;
};

export function useTableWorkspace({
  currentUserID,
  databaseName,
  selectedTableView,
  table,
  onCatalogChanged,
  onStatus
}: UseTableWorkspaceOptions) {
  const [rows, setRows] = useState<TableGridRow[]>([]);
  const [rowsViewName, setRowsViewName] = useState("all");
  const [selectedRecordID, setSelectedRecordID] = useState(0);
  const [rowHistory, setRowHistory] = useState<RowChange[]>([]);
  const [selectedRowDraft, setSelectedRowDraft] = useState<Record<string, string>>({});
  const [newFieldName, setNewFieldName] = useState("");
  const [newFieldType, setNewFieldType] = useState("text");
  const [newFieldRequired, setNewFieldRequired] = useState(false);
  const [newViewName, setNewViewName] = useState("");
  const [newViewBase, setNewViewBase] = useState("all");
  const [newViewFilterField, setNewViewFilterField] = useState("");
  const [newViewFilterOp, setNewViewFilterOp] = useState<TableViewFilter["op"]>("eq");
  const [newViewFilterValue, setNewViewFilterValue] = useState("");
  const [newViewSortField, setNewViewSortField] = useState("");
  const [newViewSortDirection, setNewViewSortDirection] = useState<TableViewSort["direction"]>("asc");

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
  const columns = useMemo(() => buildTableColumns(activeFields), [activeFields]);

  useEffect(() => {
    setSelectedRowDraft(rowDraftFromRecord(selectedRow, activeFieldNames));
  }, [activeFieldNames, selectedRow]);

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
    if (!nextRow || !previousRow || field === "record_id" || (fieldMeta?.permission_level ?? 2) < 2) {
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
      onCatalogChanged(nextCatalog, nextTable.name, nextViewName);
      resetRows(nextViewName);
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
    const nextTable = {
      ...table,
      fields: [...table.fields, { name, type: newFieldType, required: newFieldRequired, deleted: false }]
    };
    await persistTableMetadata(nextTable, `Added field ${name}`);
    setNewFieldName("");
    setNewFieldType("text");
    setNewFieldRequired(false);
  }

  async function deleteFieldFromCanvas(fieldName: string) {
    const nextTable = {
      ...table,
      fields: table.fields.map((field) => (field.name === fieldName ? { ...field, deleted: true } : field))
    };
    await persistTableMetadata(nextTable, `Deleted field ${fieldName}`);
  }

  async function createViewFromCanvas() {
    const name = newViewName.trim();
    if (!name) {
      onStatus("View name is required");
      return;
    }
    if (name === "all" || table.views.some((viewDef) => viewDef.name === name)) {
      onStatus(`View ${name} already exists`);
      return;
    }
    const filters: TableViewFilter[] = newViewFilterField
      ? [
          {
            field: newViewFilterField,
            op: newViewFilterOp,
            value: newViewFilterOp === "not_empty" ? undefined : newViewFilterValue
          }
        ]
      : [];
    const sorts: TableViewSort[] = newViewSortField
      ? [{ field: newViewSortField, direction: newViewSortDirection }]
      : [];
    const nextView: TableView = {
      name,
      display_name: name,
      base_view: newViewBase === "all" ? undefined : newViewBase,
      filters,
      sorts
    };
    const nextTable = { ...table, views: [...(table.views ?? []), nextView] };
    await persistTableMetadata(nextTable, `Created view ${name}`, name);
    setNewViewName("");
    setNewViewBase("all");
    setNewViewFilterField("");
    setNewViewFilterOp("eq");
    setNewViewFilterValue("");
    setNewViewSortField("");
    setNewViewSortDirection("asc");
  }

  async function addDraftRow() {
    if (!databaseName || !table.name) {
      onStatus("Create a table before adding rows");
      return;
    }
    const values = Object.fromEntries(activeFields.map((field) => [field.name, field.name === "status" ? "Review" : ""]));
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
      const saved = await updateRow(databaseName, table.name, selectedRecordID, selectedRowDraft);
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

  async function deleteSelectedRow() {
    if (!selectedRecordID) {
      onStatus("Select a row before deleting");
      return;
    }
    try {
      const deleted = await deleteRow(databaseName, table.name, selectedRecordID);
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
    newFieldRequired,
    newFieldType,
    newViewBase,
    newViewFilterField,
    newViewFilterOp,
    newViewFilterValue,
    newViewName,
    newViewSortDirection,
    newViewSortField,
    rowHistory,
    rows,
    selectedRecordID,
    selectedRowDraft,
    addDraftRow,
    addFieldFromCanvas,
    addSubmittedRow,
    createViewFromCanvas,
    deleteFieldFromCanvas,
    deleteSelectedRow,
    editGridRows,
    loadSelectedRowHistory,
    resetRows,
    setNewFieldName,
    setNewFieldRequired,
    setNewFieldType,
    setNewViewBase,
    setNewViewFilterField,
    setNewViewFilterOp,
    setNewViewFilterValue,
    setNewViewName,
    setNewViewSortDirection,
    setNewViewSortField,
    setSelectedRecordID,
    selectGridCell,
    updateSelectedRowDraft,
    updateSelectedRowFromEditor
  };
}
