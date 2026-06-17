import {
  Button,
  Checkbox,
  Field as FluentField,
  Input,
  Menu,
  MenuDivider,
  MenuItem,
  MenuList,
  MenuPopover,
  MenuTrigger,
  Popover,
  PopoverSurface,
  Select,
  Text,
  Toolbar,
  ToolbarButton
} from "@fluentui/react-components";
import {
  AddRegular,
  DeleteRegular,
  EditRegular,
  HistoryRegular,
  MoreHorizontalRegular,
  SaveRegular,
} from "@fluentui/react-icons";
import DataGrid, { type CellSelectArgs, type Column, type RowsChangeData } from "react-data-grid";
import { useEffect, useMemo, useState, type MouseEvent } from "react";
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
  onUpdateField: (fieldName: string, nextField: Pick<Field, "type" | "required">) => void | Promise<void>;
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
  onUpdateField,
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
  const [fieldEditor, setFieldEditor] = useState<{
    x: number;
    y: number;
    fieldName: string;
    type: string;
    required: boolean;
  } | null>(null);
  const [fieldCreator, setFieldCreator] = useState<{ x: number; y: number } | null>(null);
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
  const fieldEditorTarget = useMemo(
    () =>
      fieldEditor
        ? {
            getBoundingClientRect: () => new DOMRect(fieldEditor.x, fieldEditor.y, 0, 0)
          }
        : undefined,
    [fieldEditor]
  );
  const fieldCreatorTarget = useMemo(
    () =>
      fieldCreator
        ? {
            getBoundingClientRect: () => new DOMRect(fieldCreator.x, fieldCreator.y, 0, 0)
          }
        : undefined,
    [fieldCreator]
  );
  const gridColumns = useMemo(
    () => {
      const fieldColumns = columns.map((column) => {
        const field = activeFields.find((item) => item.name === column.key);
        if (!field) {
          return column;
        }
        return {
          ...column,
          renderHeaderCell: () => (
            <FieldHeader
              canWriteTable={canWriteTable}
              field={field}
              onDeleteField={onDeleteField}
              onEditField={(event) => {
                event.stopPropagation();
                const rect = event.currentTarget.getBoundingClientRect();
                setFieldEditor({
                  x: rect.left,
                  y: rect.bottom,
                  fieldName: field.name,
                  type: field.type,
                  required: field.required
                });
              }}
            />
          )
        } satisfies Column<TableGridRow>;
      });
      const addFieldColumn: Column<TableGridRow> = {
        key: "__add_field__",
        name: "",
        width: 48,
        minWidth: 48,
        resizable: false,
        editable: false,
        renderHeaderCell: () => (
          <button
            type="button"
            className="add-field-header-button"
            aria-label="Add field"
            disabled={!canWriteTable}
            onClick={(event) => {
              event.stopPropagation();
              const rect = event.currentTarget.getBoundingClientRect();
              onNewFieldNameChange("");
              onNewFieldTypeChange("text");
              onNewFieldRequiredChange(false);
              setFieldCreator({ x: rect.left, y: rect.bottom });
            }}
          >
            <AddRegular />
          </button>
        ),
        renderCell: () => ""
      };
      return [...fieldColumns, addFieldColumn];
    },
    [
      activeFields,
      canWriteTable,
      columns,
      onDeleteField,
      onNewFieldNameChange,
      onNewFieldRequiredChange,
      onNewFieldTypeChange
    ]
  );

  useEffect(() => {
    if (openViewPanelRequest > 0) {
      setCanvasPanel("view");
    }
  }, [openViewPanelRequest]);

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
          columns={gridColumns}
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
        <Popover
          open={Boolean(fieldEditor)}
          onOpenChange={(_, data) => {
            if (!data.open) {
              setFieldEditor(null);
            }
          }}
          positioning={fieldEditorTarget ? { target: fieldEditorTarget } : undefined}
          withArrow
        >
          <PopoverSurface className="field-editor-popover" aria-label="Edit field">
            {fieldEditor && (
              <div className="field-editor">
                <Text weight="semibold">{fieldEditor.fieldName}</Text>
                <FluentField label="Field type">
                  <Select
                    aria-label="Field type"
                    value={fieldEditor.type}
                    onChange={(_, data) =>
                      setFieldEditor((current) => (current ? { ...current, type: data.value } : current))
                    }
                  >
                    <option value="text">text</option>
                    <option value="email">email</option>
                    <option value="number">number</option>
                    <option value="date">date</option>
                  </Select>
                </FluentField>
                <Checkbox
                  label="Required"
                  checked={fieldEditor.required}
                  onChange={(_, data) =>
                    setFieldEditor((current) => (current ? { ...current, required: Boolean(data.checked) } : current))
                  }
                />
                <div className="field-editor-actions">
                  <Button onClick={() => setFieldEditor(null)}>Cancel</Button>
                  <Button
                    appearance="primary"
                    icon={<SaveRegular />}
                    onClick={() => {
                      void onUpdateField(fieldEditor.fieldName, {
                        type: fieldEditor.type,
                        required: fieldEditor.required
                      });
                      setFieldEditor(null);
                    }}
                  >
                    Save
                  </Button>
                </div>
              </div>
            )}
          </PopoverSurface>
        </Popover>
        <Popover
          open={Boolean(fieldCreator)}
          onOpenChange={(_, data) => {
            if (!data.open) {
              setFieldCreator(null);
            }
          }}
          positioning={fieldCreatorTarget ? { target: fieldCreatorTarget } : undefined}
          withArrow
        >
          <PopoverSurface className="field-editor-popover" aria-label="Add field">
            <div className="field-editor">
              <Text weight="semibold">Add field</Text>
              <FluentField label="Field name">
                <Input
                  aria-label="Field name"
                  value={newFieldName}
                  onChange={(_, data) => onNewFieldNameChange(data.value)}
                />
              </FluentField>
              <FluentField label="Field type">
                <Select
                  aria-label="New field type"
                  value={newFieldType}
                  onChange={(_, data) => onNewFieldTypeChange(data.value)}
                >
                  <option value="text">text</option>
                  <option value="email">email</option>
                  <option value="number">number</option>
                  <option value="date">date</option>
                </Select>
              </FluentField>
              <Checkbox
                label="Required"
                checked={newFieldRequired}
                onChange={(_, data) => onNewFieldRequiredChange(Boolean(data.checked))}
              />
              <div className="field-editor-actions">
                <Button onClick={() => setFieldCreator(null)}>Cancel</Button>
                <Button
                  appearance="primary"
                  icon={<AddRegular />}
                  onClick={() => {
                    void onAddField();
                    setFieldCreator(null);
                  }}
                >
                  Add
                </Button>
              </div>
            </div>
          </PopoverSurface>
        </Popover>
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

function FieldHeader({
  canWriteTable,
  field,
  onDeleteField,
  onEditField
}: {
  canWriteTable: boolean;
  field: Field;
  onDeleteField: (fieldName: string) => void;
  onEditField: (event: MouseEvent<HTMLElement>) => void;
}) {
  return (
    <div className="field-header">
      <span className="field-header-name">
        {field.name}
        {field.required ? " *" : ""}
      </span>
      <Menu>
        <MenuTrigger disableButtonEnhancement>
          <button
            type="button"
            className="field-header-menu-button"
            aria-label={`Field actions ${field.name}`}
            disabled={!canWriteTable}
            onClick={(event) => event.stopPropagation()}
          >
            <MoreHorizontalRegular />
          </button>
        </MenuTrigger>
        <MenuPopover>
          <MenuList>
            <MenuItem icon={<EditRegular />} onClick={onEditField}>
              Edit field
            </MenuItem>
            <MenuDivider />
            <MenuItem icon={<DeleteRegular />} onClick={() => onDeleteField(field.name)}>
              Delete field
            </MenuItem>
          </MenuList>
        </MenuPopover>
      </Menu>
    </div>
  );
}
