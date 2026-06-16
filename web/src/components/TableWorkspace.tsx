import {
  Select,
  Text,
  Toolbar,
  ToolbarButton,
  ToolbarDivider
} from "@fluentui/react-components";
import { AddRegular, DeleteRegular, EditRegular, HistoryRegular, TableRegular } from "@fluentui/react-icons";
import DataEditor, {
  type EditableGridCell,
  type GridCell,
  type GridColumn,
  type Item
} from "@glideapps/glide-data-grid";
import { useMemo, useState } from "react";
import type { Field, RowChange, TableMetadata, TableViewFilter, TableViewSort } from "../api";
import { TableCanvasPanel, type CanvasPanel } from "./TableCanvasPanel";

type TableWorkspaceProps = {
  columns: GridColumn[];
  displayedRecordIDs: number[];
  displayedRows: Array<Record<string, unknown>>;
  getCellContent: (cell: Item) => GridCell;
  onAddRow: () => void;
  onAddField: () => void;
  onCellEdited: (cell: Item, newValue: EditableGridCell) => void | Promise<void>;
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
  rows: Array<Record<string, unknown>>;
  selectedRecordID: number;
  selectedRowDraft: Record<string, string>;
  selectedTableView: string;
  table: TableMetadata;
};

export function TableWorkspace({
  columns,
  displayedRecordIDs,
  displayedRows,
  getCellContent,
  onAddRow,
  onAddField,
  onCellEdited,
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
        <DataEditor
          getCellContent={getCellContent}
          onCellEdited={(cell, newValue) => {
            const field = activeFields[cell[0]];
            if (!field || !canWriteField(field)) {
              return;
            }
            void onCellEdited(cell, newValue);
          }}
          onCellClicked={([, rowIndex]) => {
            const recordID = Number(displayedRows[rowIndex]?.record_id);
            if (Number.isFinite(recordID)) {
              openRecordPanel(recordID);
            }
          }}
          onHeaderClicked={(columnIndex) => {
            const field = activeFields[columnIndex];
            if (field) {
              openFieldPanel(field.name);
            }
          }}
          columns={columns}
          rows={displayedRows.length}
          rowMarkers="clickable-number"
          smoothScrollX
          smoothScrollY
          width="100%"
          height="100%"
        />
      </div>
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
  );
}

function canWriteField(field: Field): boolean {
  return (field.permission_level ?? 2) >= 2;
}
