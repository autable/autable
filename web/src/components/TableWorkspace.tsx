import {
  Select,
  Text,
  Toolbar,
  ToolbarButton,
  ToolbarDivider
} from "@fluentui/react-components";
import { AddRegular, DeleteRegular, EditRegular, HistoryRegular, TableRegular } from "@fluentui/react-icons";
import DataGrid, { type CellSelectArgs, type Column, type RowsChangeData } from "react-data-grid";
import { useMemo, useState } from "react";
import type { Field, RowChange, TableMetadata, TableViewFilter, TableViewSort } from "../api";
import type { TableGridRow } from "../tableGrid";
import { TableCanvasPanel, type CanvasPanel } from "./TableCanvasPanel";

type TableWorkspaceProps = {
  columns: Column<TableGridRow>[];
  displayedRecordIDs: number[];
  displayedRows: TableGridRow[];
  onAddRow: () => void;
  onAddField: () => void;
  onRowsChange: (rows: TableGridRow[], data: RowsChangeData<TableGridRow>) => void | Promise<void>;
  onCreateView: () => void;
  onDeleteField: (fieldName: string) => void;
  onDeleteSelectedRow: () => void;
  onLoadHistory: () => void;
  onNewFieldNameChange: (value: string) => void;
  onNewFieldRequiredChange: (value: boolean) => void;
  onNewFieldTypeChange: (value: string) => void;
  onNewViewBaseChange: (value: string) => void;
  onNewViewFilterFieldChange: (value: string) => void;
  onNewViewFilterOpChange: (value: TableViewFilter["op"]) => void;
  onNewViewFilterValueChange: (value: string) => void;
  onNewViewNameChange: (value: string) => void;
  onNewViewSortDirectionChange: (value: TableViewSort["direction"]) => void;
  onNewViewSortFieldChange: (value: string) => void;
  onSelectGridCell: (args: CellSelectArgs<TableGridRow>) => void;
  onSelectRecordID: (recordID: number) => void;
  onSelectTableView: (name: string) => void;
  onSelectedRowValueChange: (fieldName: string, value: string) => void;
  onUpdateSelectedRow: () => void;
  newFieldName: string;
  newFieldRequired: boolean;
  newFieldType: string;
  newViewBase: string;
  newViewFilterField: string;
  newViewFilterOp: TableViewFilter["op"];
  newViewFilterValue: string;
  newViewName: string;
  newViewSortDirection: TableViewSort["direction"];
  newViewSortField: string;
  rowHistory: RowChange[];
  rows: TableGridRow[];
  selectedRecordID: number;
  selectedRowDraft: Record<string, string>;
  selectedTableView: string;
  table: TableMetadata;
};

export function TableWorkspace({
  columns,
  displayedRecordIDs,
  displayedRows,
  onAddRow,
  onAddField,
  onRowsChange,
  onCreateView,
  onDeleteField,
  onDeleteSelectedRow,
  onLoadHistory,
  onNewFieldNameChange,
  onNewFieldRequiredChange,
  onNewFieldTypeChange,
  onNewViewBaseChange,
  onNewViewFilterFieldChange,
  onNewViewFilterOpChange,
  onNewViewFilterValueChange,
  onNewViewNameChange,
  onNewViewSortDirectionChange,
  onNewViewSortFieldChange,
  onSelectGridCell,
  onSelectRecordID,
  onSelectTableView,
  onSelectedRowValueChange,
  onUpdateSelectedRow,
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
  selectedTableView,
  table
}: TableWorkspaceProps) {
  const activeFields = table.fields.filter((field) => !field.deleted);
  const hasTable = Boolean(table.name);
  const canWriteTable = hasTable && (table.permission_level ?? 2) >= 2;
  const canCreateRow = activeFields.some((field) => canWriteField(field));
  const hasWritableFields = activeFields.some(canWriteField);
  const [canvasPanel, setCanvasPanel] = useState<CanvasPanel>("record");
  const [selectedFieldName, setSelectedFieldName] = useState(activeFields[0]?.name ?? "");
  const selectedField = useMemo(
    () => activeFields.find((field) => field.name === selectedFieldName) ?? activeFields[0],
    [activeFields, selectedFieldName]
  );
  const selectedView = useMemo(
    () => (table.views ?? []).find((viewDef) => viewDef.name === selectedTableView),
    [selectedTableView, table.views]
  );

  function openFieldPanel(fieldName?: string) {
    if (fieldName) {
      setSelectedFieldName(fieldName);
    }
    setCanvasPanel("fields");
  }

  function openRecordPanel(recordID?: number) {
    if (recordID && Number.isFinite(recordID)) {
      onSelectRecordID(recordID);
    }
    setCanvasPanel("record");
  }

  function loadHistoryFromCanvas() {
    setCanvasPanel("history");
    onLoadHistory();
  }

  return (
    <div className="table-view">
      <div className="section-header">
        <div>
          <Text weight="semibold">{table.display_name || table.name}</Text>
          <Text size={200}>
            {displayedRows.length} of {rows.length} records
          </Text>
        </div>
        <Toolbar aria-label="Table canvas actions" className="table-actions">
          <Select
            aria-label="Table view"
            value={selectedTableView}
            onChange={(_, data) => {
              onSelectTableView(data.value);
              setCanvasPanel("view");
            }}
          >
            <option value="all">All records</option>
            {(table.views ?? []).map((viewDef) => (
              <option key={viewDef.name} value={viewDef.name}>
                {viewDef.display_name || viewDef.name}
              </option>
            ))}
          </Select>
          <ToolbarButton icon={<TableRegular />} onClick={() => openFieldPanel()} disabled={!canWriteTable}>
            Fields
          </ToolbarButton>
          <ToolbarButton icon={<AddRegular />} onClick={() => setCanvasPanel("view")} disabled={!canWriteTable}>
            View
          </ToolbarButton>
          <ToolbarDivider />
          <ToolbarButton icon={<EditRegular />} onClick={() => openRecordPanel()} disabled={!selectedRecordID || !hasWritableFields}>
            Edit Row
          </ToolbarButton>
          <ToolbarButton icon={<HistoryRegular />} onClick={loadHistoryFromCanvas} disabled={!selectedRecordID}>
            History
          </ToolbarButton>
          <ToolbarButton icon={<DeleteRegular />} onClick={onDeleteSelectedRow} disabled={!selectedRecordID || !canWriteTable}>
            Delete Row
          </ToolbarButton>
          <ToolbarButton
            icon={<AddRegular />}
            appearance="primary"
            onClick={() => {
              setCanvasPanel("record");
              onAddRow();
            }}
            disabled={!canCreateRow}
          >
            Row
          </ToolbarButton>
        </Toolbar>
      </div>
      <div className="grid-host">
        <DataGrid
          aria-label="Table records"
          className="codetable-grid rdg-light"
          columns={columns}
          rows={displayedRows}
          rowKeyGetter={(row) => row.record_id}
          onRowsChange={(nextRows, data) => {
            void onRowsChange(nextRows, data);
          }}
          onSelectedCellChange={(args) => {
            onSelectGridCell(args);
          }}
          onCellClick={(args) => {
            const recordID = Number(args.row?.record_id);
            if (Number.isFinite(recordID)) {
              openRecordPanel(recordID);
            }
          }}
          onCellDoubleClick={(args) => {
            const field = activeFields.find((item) => item.name === args.column.key);
            if (field && canWriteField(field)) {
              openRecordPanel(Number(args.row.record_id));
            }
          }}
          onCellContextMenu={(args, event) => {
            event.preventGridDefault();
            const field = activeFields.find((item) => item.name === args.column.key);
            if (field) {
              openFieldPanel(field.name);
            }
          }}
          defaultColumnOptions={{ resizable: true }}
        />
        <TableCanvasPanel
          activeFields={activeFields}
          activePanel={canvasPanel}
          canWriteTable={canWriteTable}
          displayedRecordIDs={displayedRecordIDs}
          hasWritableFields={hasWritableFields}
          newFieldName={newFieldName}
          newFieldRequired={newFieldRequired}
          newFieldType={newFieldType}
          newViewBase={newViewBase}
          newViewFilterField={newViewFilterField}
          newViewFilterOp={newViewFilterOp}
          newViewFilterValue={newViewFilterValue}
          newViewName={newViewName}
          newViewSortDirection={newViewSortDirection}
          newViewSortField={newViewSortField}
          onAddField={onAddField}
          onCreateView={onCreateView}
          onDeleteField={onDeleteField}
          onLoadHistory={onLoadHistory}
          onNewFieldNameChange={onNewFieldNameChange}
          onNewFieldRequiredChange={onNewFieldRequiredChange}
          onNewFieldTypeChange={onNewFieldTypeChange}
          onNewViewBaseChange={onNewViewBaseChange}
          onNewViewFilterFieldChange={onNewViewFilterFieldChange}
          onNewViewFilterOpChange={onNewViewFilterOpChange}
          onNewViewFilterValueChange={onNewViewFilterValueChange}
          onNewViewNameChange={onNewViewNameChange}
          onNewViewSortDirectionChange={onNewViewSortDirectionChange}
          onNewViewSortFieldChange={onNewViewSortFieldChange}
          onOpenFields={() => openFieldPanel()}
          onOpenHistory={loadHistoryFromCanvas}
          onOpenRecord={() => openRecordPanel()}
          onOpenView={() => setCanvasPanel("view")}
          onSaveRecord={onUpdateSelectedRow}
          onSelectField={setSelectedFieldName}
          onSelectRecordID={onSelectRecordID}
          onSelectTableView={onSelectTableView}
          onSelectedRowValueChange={onSelectedRowValueChange}
          rowHistory={rowHistory}
          selectedField={selectedField}
          selectedRecordID={selectedRecordID}
          selectedRowDraft={selectedRowDraft}
          selectedView={selectedView}
          views={table.views ?? []}
        />
      </div>
    </div>
  );
}

function canWriteField(field: Field): boolean {
  return (field.permission_level ?? 2) >= 2;
}
