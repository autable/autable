import {
  Button,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
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
  Textarea,
  Toolbar,
  ToolbarButton
} from "@fluentui/react-components";
import {
  AddRegular,
  ColumnRegular,
  DeleteRegular,
  DismissRegular,
  EditRegular,
  EyeOffRegular,
  EyeRegular,
  FilterRegular,
  HistoryRegular,
  MoreHorizontalRegular,
  ReOrderDotsVerticalRegular,
  SaveRegular,
  TriangleDownFilled,
  TriangleDownRegular,
  TriangleUpFilled,
  TriangleUpRegular,
} from "@fluentui/react-icons";
import { type CellSelectArgs, type Column, type RowsChangeData } from "react-data-grid";
import { useEffect, useMemo, useState } from "react";
import { QueryBuilder, type Field as QueryBuilderField } from "react-querybuilder";
import { useTranslation } from "react-i18next";
import type { Field, RowChange, TableMetadata, TableViewQuery, TableViewSort } from "../api";
import { fieldCreatable, fieldEditable } from "../fieldPermissions";
import type { TableGridRow } from "../tableGrid";
import { RecordDataGrid } from "./RecordDataGrid";

type TableWorkspaceProps = {
  columns: Column<TableGridRow>[];
  databaseName: string;
  displayedRows: TableGridRow[];
  openViewPanelRequest: number;
  onAddRow: () => void;
  onAddField: () => void;
  onRowsChange: (rows: TableGridRow[], data: RowsChangeData<TableGridRow>) => void | Promise<void>;
  onDeleteField: (fieldName: string) => void;
  onDeleteSelectedRow: (recordID?: number) => void;
  onLoadHistory: () => void;
  onNewFieldFormulaChange: (value: string) => void;
  onNewFieldNameChange: (value: string) => void;
  onNewFieldTypeChange: (value: string) => void;
  onNewFormulaValueTypeChange: (value: string) => void;
  onNewRelationTableChange: (value: string) => void;
  onNewViewBaseChange: (value: string) => void;
  onNewViewQueryChange: (value: TableViewQuery) => void;
  onNewViewSortDirectionChange: (value: TableViewSort["direction"]) => void;
  onNewViewSortFieldChange: (value: string) => void;
  onTemporarySortChange: (value?: TableViewSort) => void;
  onMoveFieldPosition: (sourceFieldName: string, targetFieldName: string) => void | Promise<void>;
  onLoadMoreRows: () => void;
  onSearchTextChange: (value: string) => void;
  onSelectGridCell: (args: CellSelectArgs<TableGridRow>) => void;
  onSelectRecordID: (recordID: number) => void;
  onSelectedRowValueChange: (fieldName: string, value: string) => void;
  onUpdateSelectedRow: () => void;
  onUpdateSelectedView: () => void;
  onUpdateFieldFormula: (fieldName: string, formula: string) => void;
  newFieldFormula: string;
  newFieldName: string;
  newFieldType: string;
  newFormulaValueType: string;
  newRelationTable: string;
  newViewBase: string;
  newViewQuery: TableViewQuery;
  newViewSortDirection: TableViewSort["direction"];
  newViewSortField: string;
  rowHistory: RowChange[];
  relationDetail: { field: Field; table: TableMetadata; row: TableGridRow } | null;
  onCloseRelationDetail: () => void;
  rows: TableGridRow[];
  searchText: string;
  selectedRecordID: number;
  selectedRowDraft: Record<string, string>;
  selectedTableView: string;
  table: TableMetadata;
  tables: TableMetadata[];
  temporarySort?: TableViewSort;
  totalRows: number;
};

export function TableWorkspace({
  columns,
  databaseName,
  displayedRows,
  openViewPanelRequest,
  onAddRow,
  onAddField,
  onRowsChange,
  onDeleteField,
  onDeleteSelectedRow,
  onLoadHistory,
  onNewFieldFormulaChange,
  onNewFieldNameChange,
  onNewFieldTypeChange,
  onNewFormulaValueTypeChange,
  onNewRelationTableChange,
  onNewViewBaseChange,
  onNewViewQueryChange,
  onNewViewSortDirectionChange,
  onNewViewSortFieldChange,
  onTemporarySortChange,
  onMoveFieldPosition,
  onLoadMoreRows,
  onSearchTextChange,
  onSelectGridCell,
  onSelectRecordID,
  onSelectedRowValueChange,
  onUpdateSelectedRow,
  onUpdateSelectedView,
  onUpdateFieldFormula,
  newFieldFormula,
  newFieldName,
  newFieldType,
  newFormulaValueType,
  newRelationTable,
  newViewBase,
  newViewQuery,
  newViewSortDirection,
  newViewSortField,
  rowHistory,
  relationDetail,
  onCloseRelationDetail,
  rows,
  searchText,
  selectedRecordID,
  selectedRowDraft,
  selectedTableView,
  table,
  tables,
  temporarySort,
  totalRows
}: TableWorkspaceProps) {
  const { t } = useTranslation();
  const activeFields = table.fields.filter((field) => !field.deleted);
  const hasTable = Boolean(table.name);
  const canWriteDatabase = hasTable && (table.database_permission_level ?? 2) >= 2;
  const canWriteFields = hasTable && (table.field_permission_level ?? table.permission_level ?? 2) >= 2;
  const canWriteViews = hasTable && (table.view_permission_level ?? table.permission_level ?? 2) >= 2;
  const canCreateRow = selectedTableView === "all" && activeFields.some((field) => canCreateField(field));
  const hasWritableFields = activeFields.some(canWriteField);
  const [recordPanelOpen, setRecordPanelOpen] = useState(false);
  const [recordPanelTab, setRecordPanelTab] = useState<"details" | "history">("details");
  const [filterOpen, setFilterOpen] = useState(false);
  const [fieldSettingsOpen, setFieldSettingsOpen] = useState(false);
  const [fieldVisibilityState, setFieldVisibilityState] = useState<{ key: string; hiddenFieldNames: string[] }>({
    key: "",
    hiddenFieldNames: []
  });
  const [recordMenu, setRecordMenu] = useState<{ x: number; y: number; recordID: number } | null>(null);
  const [fieldCreator, setFieldCreator] = useState<{ x: number; y: number } | null>(null);
  const [formulaEditor, setFormulaEditor] = useState<{ x: number; y: number; fieldName: string; formula: string } | null>(null);
  const selectedView = useMemo(
    () => (table.views ?? []).find((viewDef) => viewDef.name === selectedTableView),
    [selectedTableView, table.views]
  );
  const canWriteSelectedView = canWriteViews || (selectedView?.permission_level ?? 0) >= 2;
  const recordMenuTarget = useMemo(
    () =>
      recordMenu
        ? {
            getBoundingClientRect: () => new DOMRect(recordMenu.x, recordMenu.y, 0, 0)
          }
        : undefined,
    [recordMenu]
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
  const formulaEditorTarget = useMemo(
    () =>
      formulaEditor
        ? {
            getBoundingClientRect: () => new DOMRect(formulaEditor.x, formulaEditor.y, 0, 0)
          }
        : undefined,
    [formulaEditor]
  );
  const activeFieldNameKey = useMemo(() => activeFields.map((field) => field.name).join("\u0000"), [activeFields]);
  const fieldVisibilityStorageKey = useMemo(
    () => createFieldVisibilityStorageKey(databaseName, table.name, selectedTableView),
    [databaseName, table.name, selectedTableView]
  );
  const hiddenFieldNames = fieldVisibilityState.key === fieldVisibilityStorageKey
    ? fieldVisibilityState.hiddenFieldNames
    : [];
  const hiddenFieldNameSet = useMemo(() => new Set(hiddenFieldNames), [hiddenFieldNames]);

  useEffect(() => {
    const activeFieldNames = activeFieldNameKey ? activeFieldNameKey.split("\u0000") : [];
    setFieldVisibilityState({
      key: fieldVisibilityStorageKey,
      hiddenFieldNames: readHiddenFieldNames(fieldVisibilityStorageKey, activeFieldNames)
    });
  }, [activeFieldNameKey, fieldVisibilityStorageKey]);

  useEffect(() => {
    if (fieldVisibilityState.key !== fieldVisibilityStorageKey) {
      return;
    }
    const activeFieldNames = activeFieldNameKey ? activeFieldNameKey.split("\u0000") : [];
    writeHiddenFieldNames(fieldVisibilityStorageKey, fieldVisibilityState.hiddenFieldNames, activeFieldNames);
  }, [activeFieldNameKey, fieldVisibilityState, fieldVisibilityStorageKey]);

  function toggleFieldVisibility(fieldName: string) {
    const activeFieldNames = activeFieldNameKey ? activeFieldNameKey.split("\u0000") : [];
    setFieldVisibilityState((current) => {
      const currentHidden = current.key === fieldVisibilityStorageKey
        ? current.hiddenFieldNames
        : readHiddenFieldNames(fieldVisibilityStorageKey, activeFieldNames);
      const nextHidden = currentHidden.includes(fieldName)
        ? currentHidden.filter((name) => name !== fieldName)
        : [...currentHidden, fieldName];
      return {
        key: fieldVisibilityStorageKey,
        hiddenFieldNames: nextHidden.filter((name) => activeFieldNames.includes(name))
      };
    });
  }

  const gridColumns = useMemo(
    () => {
      const fieldColumns = columns.filter((column) => !hiddenFieldNameSet.has(String(column.key))).map((column) => {
        const field = activeFields.find((item) => item.name === column.key);
        if (!field) {
          return column;
        }
        return {
          ...column,
          draggable: canWriteFields,
          renderHeaderCell: () => (
            <FieldHeader
              canDeleteField={canWriteDatabase}
              canWriteFields={canWriteFields}
              field={field}
              onDeleteField={onDeleteField}
              onEditFormula={(targetField, point) =>
                setFormulaEditor({
                  x: point.x,
                  y: point.y,
                  fieldName: targetField.name,
                  formula: targetField.formula ?? ""
                })
              }
              onSort={(fieldName, direction) =>
                onTemporarySortChange(direction ? { field: fieldName, direction } : undefined)
              }
              sortDirection={temporarySort?.field === field.name ? temporarySort.direction : undefined}
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
        draggable: false,
        renderHeaderCell: () => (
          <button
            type="button"
            className="add-field-header-button"
            aria-label={t("table.addField")}
            disabled={!canWriteFields}
            onClick={(event) => {
              event.stopPropagation();
              const rect = event.currentTarget.getBoundingClientRect();
              onNewFieldFormulaChange("");
              onNewFieldNameChange("");
              onNewFieldTypeChange("string");
              onNewFormulaValueTypeChange("string");
              onNewRelationTableChange("");
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
      canWriteDatabase,
      canWriteFields,
      columns,
      hiddenFieldNameSet,
      onDeleteField,
      onNewFieldFormulaChange,
      onNewFieldNameChange,
      onNewFieldTypeChange,
      onNewFormulaValueTypeChange,
      onNewRelationTableChange,
      onTemporarySortChange,
      temporarySort
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
            {t("table.recordCount", { shown: displayedRows.length, total: totalRows })}
          </Text>
        </div>
        <Toolbar aria-label={t("table.tableActions")} className="table-actions">
          <Input
            type="search"
            size="small"
            className="table-search"
            aria-label={t("table.searchRecords")}
            placeholder={t("table.searchPlaceholder")}
            value={searchText}
            onChange={(_, data) => onSearchTextChange(data.value)}
          />
          <Popover open={filterOpen} onOpenChange={(_, data) => setFilterOpen(data.open)} positioning="below-start" withArrow>
            <PopoverTrigger disableButtonEnhancement>
              <ToolbarButton icon={<FilterRegular />} disabled={!canWriteSelectedView}>
                {t("table.filter")}
              </ToolbarButton>
            </PopoverTrigger>
            <PopoverSurface className="view-filter-popover" aria-label={t("table.viewFilters")}>
              <ViewQueryPopover
                activeFields={activeFields}
                canWriteView={canWriteSelectedView}
                newViewBase={newViewBase}
                newViewQuery={newViewQuery}
                newViewSortDirection={newViewSortDirection}
                newViewSortField={newViewSortField}
                onNewViewBaseChange={onNewViewBaseChange}
                onNewViewQueryChange={onNewViewQueryChange}
                onNewViewSortDirectionChange={onNewViewSortDirectionChange}
                onNewViewSortFieldChange={onNewViewSortFieldChange}
                onSaveView={() => {
                  void onUpdateSelectedView();
                  setFilterOpen(false);
                }}
                selectedView={selectedView}
                views={table.views ?? []}
              />
            </PopoverSurface>
          </Popover>
          <ToolbarButton icon={<ColumnRegular />} onClick={() => setFieldSettingsOpen(true)}>
            {t("table.fields")}
          </ToolbarButton>
          <ToolbarButton icon={<EditRegular />} onClick={() => openRecordPanel()} disabled={!selectedRecordID || !hasWritableFields}>
            {t("table.editRow")}
          </ToolbarButton>
          <ToolbarButton icon={<HistoryRegular />} onClick={() => openHistoryPanel()} disabled={!selectedRecordID}>
            {t("common.history")}
          </ToolbarButton>
          <ToolbarButton
            icon={<AddRegular />}
            appearance="primary"
            onClick={onAddRow}
            disabled={!canCreateRow}
          >
            {t("table.row")}
          </ToolbarButton>
        </Toolbar>
      </div>
      <div className="grid-host">
        <RecordDataGrid
          aria-label={t("table.tableRecords")}
          columns={gridColumns}
          rows={displayedRows}
          rowKeyGetter={(row) => row.ct_record_id}
          onRowsChange={(nextRows, data) => {
            void onRowsChange(nextRows, data);
          }}
          onSelectedCellChange={(args) => {
            onSelectGridCell(args);
          }}
          onColumnsReorder={(sourceColumnKey, targetColumnKey) => {
            void onMoveFieldPosition(sourceColumnKey, targetColumnKey);
          }}
          onCellContextMenu={(args, event) => {
            event.preventGridDefault();
            event.preventDefault();
            const recordID = Number(args.row?.ct_record_id);
            if (Number.isFinite(recordID)) {
              onSelectRecordID(recordID);
              setRecordMenu({ x: event.clientX, y: event.clientY, recordID });
            }
          }}
          onScroll={(event) => {
            const element = event.currentTarget;
            if (element.scrollTop + element.clientHeight >= element.scrollHeight - 200) {
              onLoadMoreRows();
            }
          }}
        />
        <Dialog open={fieldSettingsOpen} onOpenChange={(_, data) => setFieldSettingsOpen(data.open)}>
          <DialogSurface className="field-settings-dialog">
            <DialogBody>
              <DialogTitle>{t("table.fieldSettings")}</DialogTitle>
              <DialogContent>
                <FieldSettingsList
                  canWriteFields={canWriteFields}
                  fields={activeFields}
                  hiddenFieldNames={hiddenFieldNames}
                  onMoveFieldPosition={onMoveFieldPosition}
                  onToggleFieldVisibility={toggleFieldVisibility}
                />
              </DialogContent>
              <DialogActions>
                <Button onClick={() => setFieldSettingsOpen(false)}>{t("common.close")}</Button>
              </DialogActions>
            </DialogBody>
          </DialogSurface>
        </Dialog>
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
                {t("table.viewDetails")}
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
                {t("table.viewHistory")}
              </MenuItem>
              <MenuDivider />
              <MenuItem
                icon={<DeleteRegular />}
                disabled={!canWriteDatabase}
                onClick={() => {
                  if (recordMenu) {
                    onDeleteSelectedRow(recordMenu.recordID);
                  }
                  setRecordMenu(null);
                }}
              >
                {t("table.deleteRecord")}
              </MenuItem>
            </MenuList>
          </MenuPopover>
        </Menu>
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
          <PopoverSurface className="field-editor-popover" aria-label={t("table.addField")}>
            <div className="field-editor">
              <Text weight="semibold">{t("table.addField")}</Text>
              <FluentField label={t("table.fieldName")}>
                <Input
                  aria-label={t("table.fieldName")}
                  value={newFieldName}
                  onChange={(_, data) => onNewFieldNameChange(data.value)}
                />
              </FluentField>
              <FluentField label={t("table.fieldType")}>
                <Select
                  aria-label={t("table.fieldType")}
                  value={newFieldType}
                  onChange={(_, data) => {
                    onNewFieldTypeChange(data.value);
                    if (data.value === "relation" && !newRelationTable) {
                      onNewRelationTableChange(tables.find((item) => item.name !== table.name)?.name ?? tables[0]?.name ?? "");
                    }
                  }}
                >
                  <option value="string">string</option>
                  <option value="int">int</option>
                  <option value="float">float</option>
                  <option value="relation">relation</option>
                  <option value="formula">formula</option>
                  <option value="file">file</option>
                </Select>
              </FluentField>
              {newFieldType === "relation" && (
                <FluentField label={t("table.targetTable")}>
                  <Select
                    aria-label={t("table.targetTable")}
                    value={newRelationTable}
                    onChange={(_, data) => onNewRelationTableChange(data.value)}
                  >
                    {tables.map((item) => (
                      <option key={item.name} value={item.name}>
                        {item.display_name || item.name}
                      </option>
                    ))}
                  </Select>
                </FluentField>
              )}
              {newFieldType === "formula" && (
                <>
                  <FluentField label={t("table.formulaValueType")}>
                    <Select
                      aria-label={t("table.formulaValueType")}
                      value={newFormulaValueType}
                      onChange={(_, data) => onNewFormulaValueTypeChange(data.value)}
                    >
                      <option value="string">string</option>
                      <option value="int">int</option>
                      <option value="float">float</option>
                    </Select>
                  </FluentField>
                  <FluentField label={t("table.formula")}>
                    <Textarea
                      aria-label={t("table.formula")}
                      value={newFieldFormula}
                      onChange={(_, data) => onNewFieldFormulaChange(data.value)}
                      placeholder={'fields["score"] + 1'}
                      resize="vertical"
                    />
                  </FluentField>
                </>
              )}
              <div className="field-editor-actions">
                <Button onClick={() => setFieldCreator(null)}>{t("common.cancel")}</Button>
                <Button
                  appearance="primary"
                  icon={<AddRegular />}
                  onClick={() => {
                    void onAddField();
                    setFieldCreator(null);
                  }}
                >
                  {t("common.add")}
                </Button>
              </div>
            </div>
          </PopoverSurface>
        </Popover>
        <Popover
          open={Boolean(formulaEditor)}
          onOpenChange={(_, data) => {
            if (!data.open) {
              setFormulaEditor(null);
            }
          }}
          positioning={formulaEditorTarget ? { target: formulaEditorTarget } : undefined}
          withArrow
        >
          <PopoverSurface className="field-editor-popover" aria-label={t("table.editFormula")}>
            <div className="field-editor formula-field-editor">
              <Text weight="semibold">{formulaEditor?.fieldName}</Text>
              <FluentField label={t("table.formula")}>
                <Textarea
                  aria-label={t("table.fieldFormula")}
                  value={formulaEditor?.formula ?? ""}
                  onChange={(_, data) =>
                    setFormulaEditor((current) => (current ? { ...current, formula: data.value } : current))
                  }
                  placeholder={'fields["score"] + 1'}
                  resize="vertical"
                />
              </FluentField>
              <div className="field-editor-actions">
                <Button onClick={() => setFormulaEditor(null)}>{t("common.cancel")}</Button>
                <Button
                  appearance="primary"
                  icon={<SaveRegular />}
                  onClick={() => {
                    if (formulaEditor) {
                      void onUpdateFieldFormula(formulaEditor.fieldName, formulaEditor.formula);
                    }
                    setFormulaEditor(null);
                  }}
                >
                  {t("common.save")}
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
        {relationDetail && (
          <RelationDrawer relation={relationDetail} onClose={onCloseRelationDetail} />
        )}
      </div>
    </div>
  );
}

function canWriteField(field: Field): boolean {
  return field.type !== "formula" && fieldEditable(field.permission_level);
}

function canCreateField(field: Field): boolean {
  return field.type !== "formula" && fieldCreatable(field.permission_level);
}

function createFieldVisibilityStorageKey(databaseName: string, tableName: string, viewName: string): string {
  return [
    "autable.fieldVisibility",
    encodeURIComponent(databaseName || "unknown"),
    encodeURIComponent(tableName || "unknown"),
    encodeURIComponent(viewName || "all")
  ].join(":");
}

function readHiddenFieldNames(storageKey: string, activeFieldNames: string[]): string[] {
  const activeFieldNameSet = new Set(activeFieldNames);
  try {
    const parsed = JSON.parse(window.localStorage.getItem(storageKey) ?? "[]");
    if (!Array.isArray(parsed)) {
      return [];
    }
    return parsed.filter((name): name is string => typeof name === "string" && activeFieldNameSet.has(name));
  } catch {
    return [];
  }
}

function writeHiddenFieldNames(storageKey: string, hiddenFieldNames: string[], activeFieldNames: string[]) {
  const activeFieldNameSet = new Set(activeFieldNames);
  const nextHiddenFieldNames = hiddenFieldNames.filter((name) => activeFieldNameSet.has(name));
  if (nextHiddenFieldNames.length === 0) {
    window.localStorage.removeItem(storageKey);
    return;
  }
  window.localStorage.setItem(storageKey, JSON.stringify(nextHiddenFieldNames));
}

function FieldSettingsList({
  canWriteFields,
  fields,
  hiddenFieldNames,
  onMoveFieldPosition,
  onToggleFieldVisibility
}: {
  canWriteFields: boolean;
  fields: Field[];
  hiddenFieldNames: string[];
  onMoveFieldPosition: (sourceFieldName: string, targetFieldName: string) => void | Promise<void>;
  onToggleFieldVisibility: (fieldName: string) => void;
}) {
  const { t } = useTranslation();
  const [draggedFieldName, setDraggedFieldName] = useState("");
  const hiddenFieldNameSet = new Set(hiddenFieldNames);
  return (
    <div className="field-settings-list" role="list">
      {fields.map((field) => {
        const hidden = hiddenFieldNameSet.has(field.name);
        return (
          <div
            className={`field-settings-row${draggedFieldName === field.name ? " is-dragging" : ""}`}
            key={field.name}
            role="listitem"
            aria-label={t("table.fieldSettingsRow", { name: field.name })}
            onDragOver={(event) => {
              if (canWriteFields && draggedFieldName && draggedFieldName !== field.name) {
                event.preventDefault();
              }
            }}
            onDrop={(event) => {
              event.preventDefault();
              const sourceFieldName = event.dataTransfer.getData("text/plain") || draggedFieldName;
              setDraggedFieldName("");
              if (canWriteFields && sourceFieldName && sourceFieldName !== field.name) {
                void onMoveFieldPosition(sourceFieldName, field.name);
              }
            }}
          >
            <button
              type="button"
              className="field-settings-drag-handle"
              aria-label={t("table.dragField", { name: field.name })}
              draggable={canWriteFields}
              disabled={!canWriteFields}
              onDragStart={(event) => {
                setDraggedFieldName(field.name);
                event.dataTransfer.effectAllowed = "move";
                event.dataTransfer.setData("text/plain", field.name);
              }}
              onDragEnd={() => setDraggedFieldName("")}
            >
              <ReOrderDotsVerticalRegular />
            </button>
            <div className="field-settings-main">
              <Text weight="semibold" truncate>
                {field.name}
              </Text>
              <Text size={200}>{field.type}</Text>
            </div>
            <div className="field-settings-actions">
              <Button
                appearance="subtle"
                icon={hidden ? <EyeOffRegular /> : <EyeRegular />}
                aria-label={hidden ? t("table.showField", { name: field.name }) : t("table.hideField", { name: field.name })}
                aria-pressed={!hidden}
                onClick={() => onToggleFieldVisibility(field.name)}
              />
            </div>
          </div>
        );
      })}
    </div>
  );
}

function RelationDrawer({
  relation,
  onClose
}: {
  relation: { field: Field; table: TableMetadata; row: TableGridRow };
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const fields = relation.table.fields.filter((field) => !field.deleted);
  return (
    <aside className="record-drawer relation-drawer" aria-label={t("table.relationRecordDetail")}>
      <div className="record-drawer-header">
        <div>
          <Text weight="semibold">{relation.table.display_name || relation.table.name}</Text>
          <Text size={200}>
            {relation.field.name} {"->"} {t("table.record")} {relation.row.ct_record_id}
          </Text>
        </div>
        <Button icon={<DismissRegular />} appearance="subtle" aria-label={t("common.close")} onClick={onClose} />
      </div>
      <div className="record-detail-list">
        <label className="field-stack">
          <span>ct_record_id</span>
          <Input aria-label={t("table.valueLabel", { name: "ct_record_id" })} readOnly value={String(relation.row.ct_record_id)} />
        </label>
        {fields.map((field) => (
          <label key={field.name} className="field-stack">
            <span>{field.name}</span>
            <Input aria-label={t("table.valueLabel", { name: field.name })} readOnly value={String(relation.row[field.name] ?? "")} />
          </label>
        ))}
      </div>
    </aside>
  );
}

function FieldHeader({
  canDeleteField,
  canWriteFields,
  field,
  onDeleteField,
  onEditFormula,
  onSort,
  sortDirection
}: {
  canDeleteField: boolean;
  canWriteFields: boolean;
  field: Field;
  onDeleteField: (fieldName: string) => void;
  onEditFormula: (field: Field, point: { x: number; y: number }) => void;
  onSort: (fieldName: string, direction?: TableViewSort["direction"]) => void;
  sortDirection?: TableViewSort["direction"];
}) {
  const { t } = useTranslation();
  const nextSortDirection = sortDirection === "desc" ? "asc" : sortDirection === "asc" ? undefined : "desc";
  const SortUpIcon = sortDirection === "asc" ? TriangleUpFilled : TriangleUpRegular;
  const SortDownIcon = sortDirection === "desc" ? TriangleDownFilled : TriangleDownRegular;
  return (
    <div className="field-header">
      <span className="field-header-name">{field.name}</span>
      <button
        type="button"
        className={`field-header-sort-button${sortDirection ? " is-active" : ""}`}
        aria-label={t("table.toggleSort", { name: field.name })}
        aria-pressed={Boolean(sortDirection)}
        title={t("table.toggleSort", { name: field.name })}
        onClick={(event) => {
          event.preventDefault();
          event.stopPropagation();
          onSort(field.name, nextSortDirection);
        }}
      >
        <span className="field-header-sort-icons">
          <SortUpIcon className={sortDirection === "asc" ? "is-active" : ""} />
          <SortDownIcon className={sortDirection === "desc" ? "is-active" : ""} />
        </span>
      </button>
      <Menu>
        <MenuTrigger disableButtonEnhancement>
          <button
            type="button"
            className="field-header-menu-button"
            aria-label={t("table.fieldActions", { name: field.name })}
            disabled={!canWriteFields && !canDeleteField}
            onClick={(event) => event.stopPropagation()}
          >
            <MoreHorizontalRegular />
          </button>
        </MenuTrigger>
        <MenuPopover>
          <MenuList>
            {field.type === "formula" && (
              <MenuItem
                icon={<EditRegular />}
                disabled={!canWriteFields}
                onClick={(event) => {
                  const rect = event.currentTarget.getBoundingClientRect();
                  onEditFormula(field, { x: rect.left, y: rect.bottom });
                }}
              >
                {t("table.editFormula")}
              </MenuItem>
            )}
            <MenuItem icon={<DeleteRegular />} disabled={!canDeleteField} onClick={() => onDeleteField(field.name)}>
              {t("table.deleteField")}
            </MenuItem>
          </MenuList>
        </MenuPopover>
      </Menu>
    </div>
  );
}

function ViewQueryPopover({
  activeFields,
  canWriteView,
  newViewBase,
  newViewQuery,
  newViewSortDirection,
  newViewSortField,
  onNewViewBaseChange,
  onNewViewQueryChange,
  onNewViewSortDirectionChange,
  onNewViewSortFieldChange,
  onSaveView,
  selectedView,
  views
}: {
  activeFields: Field[];
  canWriteView: boolean;
  newViewBase: string;
  newViewQuery: TableViewQuery;
  newViewSortDirection: TableViewSort["direction"];
  newViewSortField: string;
  onNewViewBaseChange: (value: string) => void;
  onNewViewQueryChange: (value: TableViewQuery) => void;
  onNewViewSortDirectionChange: (value: TableViewSort["direction"]) => void;
  onNewViewSortFieldChange: (value: string) => void;
  onSaveView: () => void;
  selectedView?: NonNullable<TableMetadata["views"]>[number];
  views: NonNullable<TableMetadata["views"]>;
}) {
  const { t } = useTranslation();
  const canEditView = canWriteView && Boolean(selectedView);
  const queryFields = useMemo<QueryBuilderField[]>(
    () => [
      { name: "ct_record_id", label: "ct_record_id", inputType: "number" },
      ...activeFields.map((field) => ({
        name: field.name,
        label: field.name,
        inputType: field.type === "int" || field.type === "float" || field.value_type === "int" || field.value_type === "float" ? "number" : "text"
      }))
    ],
    [activeFields]
  );
  return (
    <div className="view-filter-editor">
      <div className="view-filter-header">
        <Text weight="semibold">{selectedView?.display_name || selectedView?.name || t("common.allRecords")}</Text>
        <Text size={200}>
          {selectedView?.base_view
            ? t("table.basedOn", { view: selectedView.base_view })
            : selectedView
              ? t("table.tableView")
              : t("common.baseTable")}
        </Text>
      </div>
      <FluentField label={t("table.baseView")}>
        <Select
          aria-label={t("table.baseView")}
          value={newViewBase}
          onChange={(_, data) => onNewViewBaseChange(data.value)}
          disabled={!canEditView}
        >
          <option value="all">{t("common.allRecords")}</option>
          {views.filter((item) => item.name !== selectedView?.name).map((item) => (
            <option key={item.name} value={item.name}>
              {item.display_name || item.name}
            </option>
          ))}
        </Select>
      </FluentField>
      <FluentField label={t("table.viewFilters")}>
        <div className="view-query-builder" aria-disabled={!canEditView}>
          <QueryBuilder
            fields={queryFields}
            query={newViewQuery as never}
            onQueryChange={(query: unknown) => onNewViewQueryChange(query as TableViewQuery)}
            disabled={!canEditView}
            showNotToggle
            resetOnFieldChange={false}
            operators={[
              { name: "=", label: "=" },
              { name: "!=", label: "!=" },
              { name: "<", label: "<" },
              { name: "<=", label: "<=" },
              { name: ">", label: ">" },
              { name: ">=", label: ">=" },
              { name: "contains", label: t("table.operators.contains") },
              { name: "beginsWith", label: t("table.operators.beginsWith") },
              { name: "endsWith", label: t("table.operators.endsWith") },
              { name: "doesNotContain", label: t("table.operators.doesNotContain") },
              { name: "null", label: t("table.operators.empty") },
              { name: "notNull", label: t("table.operators.notEmpty") }
            ]}
            translations={{
              addRule: { label: t("table.addFilterRule"), title: t("table.addFilterRule") },
              addGroup: { label: t("table.addFilterGroup"), title: t("table.addFilterGroup") },
              removeRule: { label: t("common.delete"), title: t("common.delete") },
              removeGroup: { label: t("common.delete"), title: t("common.delete") }
            }}
          />
        </div>
      </FluentField>
      <div className="view-filter-grid">
        <FluentField label={t("table.sortField")}>
          <Select
            aria-label={t("table.viewSortField")}
            value={newViewSortField}
            onChange={(_, data) => onNewViewSortFieldChange(data.value)}
            disabled={!canEditView}
          >
            <option value="">{t("table.noSort")}</option>
            <option value="ct_record_id">ct_record_id</option>
            {activeFields.map((field) => (
              <option key={field.name} value={field.name}>
                {field.name}
              </option>
            ))}
          </Select>
        </FluentField>
        <FluentField label={t("table.sortDirection")}>
          <Select
            aria-label={t("table.viewSortDirection")}
            value={newViewSortDirection}
            onChange={(_, data) => onNewViewSortDirectionChange(data.value as TableViewSort["direction"])}
            disabled={!canEditView || !newViewSortField}
          >
            <option value="asc">{t("table.ascending")}</option>
            <option value="desc">{t("table.descending")}</option>
          </Select>
        </FluentField>
      </div>
      <Button appearance="primary" icon={<SaveRegular />} onClick={onSaveView} disabled={!canEditView}>
        {t("table.saveView")}
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
  const { t } = useTranslation();
  return (
    <aside className="record-drawer" aria-label={t("table.recordPanel")}>
      <div className="record-drawer-header">
        <div>
          <Text weight="semibold">
            {selectedRecordID ? t("table.recordTitle", { id: selectedRecordID }) : t("table.noRecordSelected")}
          </Text>
          <Text size={200}>{hasWritableFields ? t("table.writableFields") : t("table.readOnly")}</Text>
        </div>
        <Button appearance="subtle" icon={<DismissRegular />} aria-label={t("common.close")} onClick={onClose} />
      </div>
      <TabList
        aria-label={t("table.recordTabs")}
        appearance="subtle"
        selectedValue={tab}
        onTabSelect={(_, data) => onTabChange(data.value as "details" | "history")}
      >
        <Tab value="details">{t("common.details")}</Tab>
        <Tab value="history">{t("common.history")}</Tab>
      </TabList>
      {tab === "details" ? (
        <div className="record-detail-list">
          {fields.map((field) => (
            <FluentField key={field.name} label={field.name}>
              <Input
                aria-label={t("table.valueLabel", { name: field.name })}
                value={values[field.name] ?? ""}
                onChange={(_, data) => onChange(field.name, data.value)}
                disabled={!selectedRecordID || !canWriteField(field)}
              />
            </FluentField>
          ))}
          <Button appearance="primary" icon={<SaveRegular />} onClick={onSave} disabled={!selectedRecordID || !hasWritableFields}>
            {t("table.saveRow")}
          </Button>
        </div>
      ) : (
        <div className="record-history-pane" aria-label={t("table.rowHistory")}>
          <div className="record-history-toolbar">
            <Text size={200}>
              {selectedRecordID ? t("table.recordTitle", { id: selectedRecordID }) : t("table.noRecordSelected")}
            </Text>
            <Button onClick={onLoadHistory} disabled={!selectedRecordID}>
              {t("common.refresh")}
            </Button>
          </div>
          <RowHistoryList rowHistory={rowHistory} />
        </div>
      )}
    </aside>
  );
}

function RowHistoryList({ rowHistory }: { rowHistory: RowChange[] }) {
  const { t } = useTranslation();
  if (rowHistory.length === 0) {
    return <Text size={200}>{t("table.noRowHistoryLoaded")}</Text>;
  }
  return (
    <div className="row-history-list">
      {rowHistory.map((change) => (
        <div key={change.history_key} className="row-history-entry">
          <div>
            <Text weight="semibold">{friendlyHistoryOperation(change.operation, t)}</Text>
            <Text size={200}>
              {[formatHistoryTime(change.timestamp), change.actor_id ? t("table.byActor", { actor: change.actor_id }) : ""].filter(Boolean).join(" · ")}
            </Text>
          </div>
          <HistoryJSON title={t("table.diff")} value={change.diff ?? {}} />
          <HistoryJSON title={t("table.values")} value={change.values} />
        </div>
      ))}
    </div>
  );
}

function HistoryJSON({ title, value }: { title: string; value: unknown }) {
  return (
    <div>
      <Text size={200} weight="semibold">
        {title}
      </Text>
      <pre>{JSON.stringify(value, null, 2)}</pre>
    </div>
  );
}

function friendlyHistoryOperation(operation: string | undefined, t: ReturnType<typeof useTranslation>["t"]): string {
  if (operation === "create") {
    return t("table.operations.created");
  }
  if (operation === "update") {
    return t("table.operations.updated");
  }
  if (operation === "delete") {
    return t("table.operations.deleted");
  }
  return t("table.recordChange");
}

function formatHistoryTime(timestamp: number): string {
  const parsed = new Date(timestamp);
  if (Number.isNaN(parsed.getTime())) {
    return String(timestamp);
  }
  return parsed.toLocaleString();
}
