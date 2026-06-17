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
  PopoverTrigger,
  Select,
  Tab,
  TabList,
  Text,
  Toolbar,
  ToolbarButton
} from "@fluentui/react-components";
import {
  AddRegular,
  DeleteRegular,
  DismissRegular,
  EditRegular,
  FilterRegular,
  HistoryRegular,
  MoreHorizontalRegular,
  SaveRegular,
} from "@fluentui/react-icons";
import DataGrid, { type CellSelectArgs, type Column, type RowsChangeData } from "react-data-grid";
import { useEffect, useMemo, useState, type MouseEvent } from "react";
import type { Field, RowChange, TableMetadata, TableViewFilter, TableViewSort } from "../api";
import type { TableGridRow } from "../tableGrid";

type TableWorkspaceProps = {
  columns: Column<TableGridRow>[];
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
  const [recordPanelOpen, setRecordPanelOpen] = useState(false);
  const [recordPanelTab, setRecordPanelTab] = useState<"details" | "history">("details");
  const [filterOpen, setFilterOpen] = useState(false);
  const [recordMenu, setRecordMenu] = useState<{ x: number; y: number; recordID: number } | null>(null);
  const [fieldEditor, setFieldEditor] = useState<{
    x: number;
    y: number;
    fieldName: string;
    type: string;
    required: boolean;
  } | null>(null);
  const [fieldCreator, setFieldCreator] = useState<{ x: number; y: number } | null>(null);
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
      setFilterOpen(true);
    }
  }, [openViewPanelRequest]);

  function openRecordPanel(recordID?: number) {
    if (recordID && Number.isFinite(recordID)) {
      onSelectRecordID(recordID);
    }
    setRecordPanelTab("details");
    setRecordPanelOpen(true);
  }

  function openHistoryPanel(recordID?: number) {
    if (recordID && Number.isFinite(recordID)) {
      onSelectRecordID(recordID);
    }
    setRecordPanelTab("history");
    setRecordPanelOpen(true);
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
          <Popover open={filterOpen} onOpenChange={(_, data) => setFilterOpen(data.open)} positioning="below-start" withArrow>
            <PopoverTrigger disableButtonEnhancement>
              <ToolbarButton icon={<FilterRegular />} disabled={!canWriteTable}>
                Filter
              </ToolbarButton>
            </PopoverTrigger>
            <PopoverSurface className="view-filter-popover" aria-label="View filters">
              <ViewFilterPopover
                activeFields={activeFields}
                canWriteTable={canWriteTable}
                newViewBase={newViewBase}
                newViewFilterField={newViewFilterField}
                newViewFilterOp={newViewFilterOp}
                newViewFilterValue={newViewFilterValue}
                newViewSortDirection={newViewSortDirection}
                newViewSortField={newViewSortField}
                onNewViewBaseChange={onNewViewBaseChange}
                onNewViewFilterFieldChange={onNewViewFilterFieldChange}
                onNewViewFilterOpChange={onNewViewFilterOpChange}
                onNewViewFilterValueChange={onNewViewFilterValueChange}
                onNewViewSortDirectionChange={onNewViewSortDirectionChange}
                onNewViewSortFieldChange={onNewViewSortFieldChange}
                onSaveView={onUpdateSelectedView}
                selectedView={selectedView}
                views={table.views ?? []}
              />
            </PopoverSurface>
          </Popover>
          <ToolbarButton icon={<EditRegular />} onClick={() => openRecordPanel()} disabled={!selectedRecordID || !hasWritableFields}>
            Edit Row
          </ToolbarButton>
          <ToolbarButton icon={<HistoryRegular />} onClick={() => openHistoryPanel()} disabled={!selectedRecordID}>
            History
          </ToolbarButton>
          <ToolbarButton
            icon={<AddRegular />}
            appearance="primary"
            onClick={onAddRow}
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
                icon={<EditRegular />}
                onClick={() => {
                  if (recordMenu) {
                    openRecordPanel(recordMenu.recordID);
                  }
                  setRecordMenu(null);
                }}
              >
                View details
              </MenuItem>
              <MenuItem
                icon={<HistoryRegular />}
                onClick={() => {
                  if (recordMenu) {
                    openHistoryPanel(recordMenu.recordID);
                  }
                  setRecordMenu(null);
                }}
              >
                View history
              </MenuItem>
              <MenuDivider />
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
        {recordPanelOpen && (
          <RecordDrawer
            fields={activeFields}
            hasWritableFields={hasWritableFields}
            onChange={onSelectedRowValueChange}
            onClose={() => setRecordPanelOpen(false)}
            onLoadHistory={onLoadHistory}
            onSave={onUpdateSelectedRow}
            onTabChange={(tab) => {
              setRecordPanelTab(tab);
              if (tab === "history") {
                onLoadHistory();
              }
            }}
            rowHistory={rowHistory}
            selectedRecordID={selectedRecordID}
            tab={recordPanelTab}
            values={selectedRowDraft}
          />
        )}
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

function ViewFilterPopover({
  activeFields,
  canWriteTable,
  newViewBase,
  newViewFilterField,
  newViewFilterOp,
  newViewFilterValue,
  newViewSortDirection,
  newViewSortField,
  onNewViewBaseChange,
  onNewViewFilterFieldChange,
  onNewViewFilterOpChange,
  onNewViewFilterValueChange,
  onNewViewSortDirectionChange,
  onNewViewSortFieldChange,
  onSaveView,
  selectedView,
  views
}: {
  activeFields: Field[];
  canWriteTable: boolean;
  newViewBase: string;
  newViewFilterField: string;
  newViewFilterOp: TableViewFilter["op"];
  newViewFilterValue: string;
  newViewSortDirection: TableViewSort["direction"];
  newViewSortField: string;
  onNewViewBaseChange: (value: string) => void;
  onNewViewFilterFieldChange: (value: string) => void;
  onNewViewFilterOpChange: (value: TableViewFilter["op"]) => void;
  onNewViewFilterValueChange: (value: string) => void;
  onNewViewSortDirectionChange: (value: TableViewSort["direction"]) => void;
  onNewViewSortFieldChange: (value: string) => void;
  onSaveView: () => void;
  selectedView?: NonNullable<TableMetadata["views"]>[number];
  views: NonNullable<TableMetadata["views"]>;
}) {
  const canEditView = canWriteTable && Boolean(selectedView);
  return (
    <div className="view-filter-editor">
      <div className="view-filter-header">
        <Text weight="semibold">{selectedView?.display_name || selectedView?.name || "All records"}</Text>
        <Text size={200}>
          {selectedView?.base_view ? `based on ${selectedView.base_view}` : selectedView ? "table view" : "base table"}
        </Text>
      </div>
      <FluentField label="Base view">
        <Select
          aria-label="Base view"
          value={newViewBase}
          onChange={(_, data) => onNewViewBaseChange(data.value)}
          disabled={!canEditView}
        >
          <option value="all">All records</option>
          {views.filter((item) => item.name !== selectedView?.name).map((item) => (
            <option key={item.name} value={item.name}>
              {item.display_name || item.name}
            </option>
          ))}
        </Select>
      </FluentField>
      <div className="view-filter-grid">
        <FluentField label="Filter field">
          <Select
            aria-label="View filter field"
            value={newViewFilterField}
            onChange={(_, data) => onNewViewFilterFieldChange(data.value)}
            disabled={!canEditView}
          >
            <option value="">No filter</option>
            {activeFields.map((field) => (
              <option key={field.name} value={field.name}>
                {field.name}
              </option>
            ))}
          </Select>
        </FluentField>
        <FluentField label="Filter operator">
          <Select
            aria-label="View filter operator"
            value={newViewFilterOp}
            onChange={(_, data) => onNewViewFilterOpChange(data.value as TableViewFilter["op"])}
            disabled={!canEditView || !newViewFilterField}
          >
            <option value="eq">equals</option>
            <option value="contains">contains</option>
            <option value="not_empty">not empty</option>
          </Select>
        </FluentField>
      </div>
      <FluentField label="Filter value">
        <Input
          aria-label="View filter value"
          value={newViewFilterValue}
          onChange={(_, data) => onNewViewFilterValueChange(data.value)}
          disabled={!canEditView || !newViewFilterField || newViewFilterOp === "not_empty"}
        />
      </FluentField>
      <div className="view-filter-grid">
        <FluentField label="Sort field">
          <Select
            aria-label="View sort field"
            value={newViewSortField}
            onChange={(_, data) => onNewViewSortFieldChange(data.value)}
            disabled={!canEditView}
          >
            <option value="">No sort</option>
            <option value="record_id">record_id</option>
            {activeFields.map((field) => (
              <option key={field.name} value={field.name}>
                {field.name}
              </option>
            ))}
          </Select>
        </FluentField>
        <FluentField label="Sort direction">
          <Select
            aria-label="View sort direction"
            value={newViewSortDirection}
            onChange={(_, data) => onNewViewSortDirectionChange(data.value as TableViewSort["direction"])}
            disabled={!canEditView || !newViewSortField}
          >
            <option value="asc">ascending</option>
            <option value="desc">descending</option>
          </Select>
        </FluentField>
      </div>
      <Button appearance="primary" icon={<SaveRegular />} onClick={onSaveView} disabled={!canEditView}>
        Save View
      </Button>
    </div>
  );
}

function RecordDrawer({
  fields,
  hasWritableFields,
  onChange,
  onClose,
  onLoadHistory,
  onSave,
  onTabChange,
  rowHistory,
  selectedRecordID,
  tab,
  values
}: {
  fields: Field[];
  hasWritableFields: boolean;
  onChange: (fieldName: string, value: string) => void;
  onClose: () => void;
  onLoadHistory: () => void;
  onSave: () => void;
  onTabChange: (tab: "details" | "history") => void;
  rowHistory: RowChange[];
  selectedRecordID: number;
  tab: "details" | "history";
  values: Record<string, string>;
}) {
  return (
    <aside className="record-drawer" aria-label="Record panel">
      <div className="record-drawer-header">
        <div>
          <Text weight="semibold">{selectedRecordID ? `record #${selectedRecordID}` : "No record selected"}</Text>
          <Text size={200}>{hasWritableFields ? "Writable fields" : "Read only"}</Text>
        </div>
        <Button appearance="subtle" icon={<DismissRegular />} aria-label="Close record panel" onClick={onClose} />
      </div>
      <TabList
        aria-label="Record tabs"
        appearance="subtle"
        selectedValue={tab}
        onTabSelect={(_, data) => onTabChange(data.value as "details" | "history")}
      >
        <Tab value="details">Details</Tab>
        <Tab value="history">History</Tab>
      </TabList>
      {tab === "details" ? (
        <div className="record-detail-list">
          {fields.map((field) => (
            <FluentField key={field.name} label={field.name}>
              <Input
                aria-label={`${field.name} value`}
                value={values[field.name] ?? ""}
                onChange={(_, data) => onChange(field.name, data.value)}
                disabled={!selectedRecordID || !canWriteField(field)}
              />
            </FluentField>
          ))}
          <Button appearance="primary" icon={<SaveRegular />} onClick={onSave} disabled={!selectedRecordID || !hasWritableFields}>
            Save Row
          </Button>
        </div>
      ) : (
        <div className="record-history-pane" aria-label="Row history">
          <div className="record-history-toolbar">
            <Text size={200}>{selectedRecordID ? `record #${selectedRecordID}` : "No record selected"}</Text>
            <Button onClick={onLoadHistory} disabled={!selectedRecordID}>
              Refresh
            </Button>
          </div>
          <RowHistoryList rowHistory={rowHistory} />
        </div>
      )}
    </aside>
  );
}

function RowHistoryList({ rowHistory }: { rowHistory: RowChange[] }) {
  if (rowHistory.length === 0) {
    return <Text size={200}>No row history loaded</Text>;
  }
  return (
    <div className="row-history-list">
      {rowHistory.map((change) => (
        <div key={change.history_key} className="row-history-entry">
          <div>
            <Text weight="semibold">{friendlyHistoryOperation(change.operation)}</Text>
            <Text size={200}>
              {[formatHistoryTime(change.timestamp), change.actor_id ? `by ${change.actor_id}` : ""].filter(Boolean).join(" · ")}
            </Text>
          </div>
          <pre>{JSON.stringify(change.values, null, 2)}</pre>
        </div>
      ))}
    </div>
  );
}

function friendlyHistoryOperation(operation?: string): string {
  if (operation === "create") {
    return "Created";
  }
  if (operation === "update") {
    return "Updated";
  }
  if (operation === "delete") {
    return "Deleted";
  }
  return "Record change";
}

function formatHistoryTime(timestamp: string): string {
  const parsed = new Date(timestamp);
  if (Number.isNaN(parsed.getTime())) {
    return timestamp;
  }
  return parsed.toLocaleString();
}
