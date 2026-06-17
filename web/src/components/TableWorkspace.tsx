import {
  Menu,
  MenuItem,
  MenuList,
  MenuPopover,
  Text,
  Toolbar,
  ToolbarButton,
  ToolbarDivider
} from "@fluentui/react-components";
import { AddRegular, DeleteRegular, EditRegular, HistoryRegular, TableRegular } from "@fluentui/react-icons";
import DataGrid, { type CellSelectArgs, type Column, type RowsChangeData } from "react-data-grid";
import { useEffect, useMemo, useState } from "react";
import type { Field, RowChange, TableMetadata, TableViewFilter, TableViewSort } from "../api";
import type { TableGridRow } from "../tableGrid";
import { TableCanvasPanel, type CanvasPanel } from "./TableCanvasPanel";

type TableWorkspaceProps = {
  columns: Column<TableGridRow>[];
  displayedRecordIDs: number[];
  displayedRows: TableGridRow[];
  openViewPanelRequest: number;
  onAddRow: () => void;
  onAddField: () => void;
  onRowsChange: (rows: TableGridRow[], data: RowsChangeData<TableGridRow>) => void | Promise<void>;
  onDeleteField: (fieldName: string) => void;
  onDeleteSelectedRow: (recordID?: number) => void;
  onLoadHistory: () => void;
  onNewFieldNameChange: (value: string) => void;
  onNewFieldRequiredChange: (value: boolean) => void;
  onNewFieldTypeChange: (value: string) => void;
  onNewViewBaseChange: (value: string) => void;
  onNewViewFilterFieldChange: (value: string) => void;
  onNewViewFilterOpChange: (value: TableViewFilter["op"]) => void;
  onNewViewFilterValueChange: (value: string) => void;
  onNewViewSortDirectionChange: (value: TableViewSort["direction"]) => void;
  onNewViewSortFieldChange: (value: string) => void;
  onSelectGridCell: (args: CellSelectArgs<TableGridRow>) => void;
  onSelectRecordID: (recordID: number) => void;
  onSelectTableView: (name: string) => void;
  onSelectedRowValueChange: (fieldName: string, value: string) => void;
  onUpdateSelectedRow: () => void;
  onUpdateSelectedView: () => void;
  newFieldName: string;
  newFieldRequired: boolean;
  newFieldType: string;
  newViewBase: string;
  newViewFilterField: string;
  newViewFilterOp: TableViewFilter["op"];
  newViewFilterValue: string;
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
  openViewPanelRequest,
  onAddRow,
  onAddField,
  onRowsChange,
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
  onNewViewSortDirectionChange,
  onNewViewSortFieldChange,
  onSelectGridCell,
  onSelectRecordID,
  onSelectTableView,
  onSelectedRowValueChange,
  onUpdateSelectedRow,
  onUpdateSelectedView,
  newFieldName,
  newFieldRequired,
  newFieldType,
  newViewBase,
  newViewFilterField,
  newViewFilterOp,
  newViewFilterValue,
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
  const [recordMenu, setRecordMenu] = useState<{ x: number; y: number; recordID: number } | null>(null);
  const selectedField = useMemo(
    () => activeFields.find((field) => field.name === selectedFieldName) ?? activeFields[0],
    [activeFields, selectedFieldName]
  );
  const selectedView = useMemo(
    () => (table.views ?? []).find((viewDef) => viewDef.name === selectedTableView),
    [selectedTableView, table.views]
  );
  const recordMenuTarget = useMemo(
    () =>
      recordMenu
        ? {
            getBoundingClientRect: () => new DOMRect(recordMenu.x, recordMenu.y, 0, 0)
          }
        : undefined,
    [recordMenu]
  );

  useEffect(() => {
    if (openViewPanelRequest > 0) {
      setCanvasPanel("view");
    }
  }, [openViewPanelRequest]);

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
          <ToolbarButton icon={<TableRegular />} onClick={() => openFieldPanel()} disabled={!canWriteTable}>
            Fields
          </ToolbarButton>
          <ToolbarButton icon={<EditRegular />} onClick={() => openRecordPanel()} disabled={!selectedRecordID || !hasWritableFields}>
            Edit Row
          </ToolbarButton>
          <ToolbarButton icon={<HistoryRegular />} onClick={loadHistoryFromCanvas} disabled={!selectedRecordID}>
            History
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
            event.preventDefault();
            const recordID = Number(args.row?.record_id);
            if (Number.isFinite(recordID)) {
              onSelectRecordID(recordID);
              setRecordMenu({ x: event.clientX, y: event.clientY, recordID });
            }
          }}
          defaultColumnOptions={{ resizable: true }}
        />
        <Menu
          open={Boolean(recordMenu)}
          onOpenChange={(_, data) => {
            if (!data.open) {
              setRecordMenu(null);
            }
          }}
          positioning={recordMenuTarget ? { target: recordMenuTarget } : undefined}
        >
          <MenuPopover>
            <MenuList>
              <MenuItem
                icon={<DeleteRegular />}
                disabled={!canWriteTable}
                onClick={() => {
                  if (recordMenu) {
                    onDeleteSelectedRow(recordMenu.recordID);
                  }
                  setRecordMenu(null);
                }}
              >
                Delete record
              </MenuItem>
            </MenuList>
          </MenuPopover>
        </Menu>
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
          newViewSortDirection={newViewSortDirection}
          newViewSortField={newViewSortField}
          onAddField={onAddField}
          onDeleteField={onDeleteField}
          onLoadHistory={onLoadHistory}
          onNewFieldNameChange={onNewFieldNameChange}
          onNewFieldRequiredChange={onNewFieldRequiredChange}
          onNewFieldTypeChange={onNewFieldTypeChange}
          onNewViewBaseChange={onNewViewBaseChange}
          onNewViewFilterFieldChange={onNewViewFilterFieldChange}
          onNewViewFilterOpChange={onNewViewFilterOpChange}
          onNewViewFilterValueChange={onNewViewFilterValueChange}
          onNewViewSortDirectionChange={onNewViewSortDirectionChange}
          onNewViewSortFieldChange={onNewViewSortFieldChange}
          onOpenFields={() => openFieldPanel()}
          onOpenHistory={loadHistoryFromCanvas}
          onOpenRecord={() => openRecordPanel()}
          onOpenView={() => setCanvasPanel("view")}
          onSaveRecord={onUpdateSelectedRow}
          onSaveView={onUpdateSelectedView}
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
