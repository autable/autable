import {
  Button,
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
  DeleteRegular,
  DismissRegular,
  EditRegular,
  FilterRegular,
  HistoryRegular,
  MoreHorizontalRegular,
  SaveRegular,
} from "@fluentui/react-icons";
import DataGrid, { type CellSelectArgs, type Column, type RowsChangeData } from "react-data-grid";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
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
  onNewFieldFormulaChange: (value: string) => void;
  onNewFieldNameChange: (value: string) => void;
  onNewFieldTypeChange: (value: string) => void;
  onNewFormulaValueTypeChange: (value: string) => void;
  onNewRelationTableChange: (value: string) => void;
  onNewViewBaseChange: (value: string) => void;
  onNewViewFilterFieldChange: (value: string) => void;
  onNewViewFilterOpChange: (value: TableViewFilter["op"]) => void;
  onNewViewFilterValueChange: (value: string) => void;
  onNewViewSortDirectionChange: (value: TableViewSort["direction"]) => void;
  onNewViewSortFieldChange: (value: string) => void;
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
  newViewFilterField: string;
  newViewFilterOp: TableViewFilter["op"];
  newViewFilterValue: string;
  newViewSortDirection: TableViewSort["direction"];
  newViewSortField: string;
  rowHistory: RowChange[];
  relationDetail: { field: Field; table: TableMetadata; row: TableGridRow } | null;
  onCloseRelationDetail: () => void;
  rows: TableGridRow[];
  selectedRecordID: number;
  selectedRowDraft: Record<string, string>;
  selectedTableView: string;
  table: TableMetadata;
  tables: TableMetadata[];
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
  onNewFieldFormulaChange,
  onNewFieldNameChange,
  onNewFieldTypeChange,
  onNewFormulaValueTypeChange,
  onNewRelationTableChange,
  onNewViewBaseChange,
  onNewViewFilterFieldChange,
  onNewViewFilterOpChange,
  onNewViewFilterValueChange,
  onNewViewSortDirectionChange,
  onNewViewSortFieldChange,
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
  newViewFilterField,
  newViewFilterOp,
  newViewFilterValue,
  newViewSortDirection,
  newViewSortField,
  rowHistory,
  relationDetail,
  onCloseRelationDetail,
  rows,
  selectedRecordID,
  selectedRowDraft,
  selectedTableView,
  table,
  tables
}: TableWorkspaceProps) {
  const { t } = useTranslation();
  const activeFields = table.fields.filter((field) => !field.deleted);
  const hasTable = Boolean(table.name);
  const canWriteTable = hasTable && (table.permission_level ?? 2) >= 2;
  const canCreateRow = activeFields.some((field) => canWriteField(field));
  const hasWritableFields = activeFields.some(canWriteField);
  const [recordPanelOpen, setRecordPanelOpen] = useState(false);
  const [recordPanelTab, setRecordPanelTab] = useState<"details" | "history">("details");
  const [filterOpen, setFilterOpen] = useState(false);
  const [recordMenu, setRecordMenu] = useState<{ x: number; y: number; recordID: number } | null>(null);
  const [fieldCreator, setFieldCreator] = useState<{ x: number; y: number } | null>(null);
  const [formulaEditor, setFormulaEditor] = useState<{ x: number; y: number; fieldName: string; formula: string } | null>(null);
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
              onEditFormula={(targetField, point) =>
                setFormulaEditor({
                  x: point.x,
                  y: point.y,
                  fieldName: targetField.name,
                  formula: targetField.formula ?? ""
                })
              }
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
            aria-label={t("table.addField")}
            disabled={!canWriteTable}
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
      canWriteTable,
      columns,
      onDeleteField,
      onNewFieldFormulaChange,
      onNewFieldNameChange,
      onNewFieldTypeChange,
      onNewFormulaValueTypeChange,
      onNewRelationTableChange
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
            {t("table.recordCount", { shown: displayedRows.length, total: rows.length })}
          </Text>
        </div>
        <Toolbar aria-label={t("table.tableActions")} className="table-actions">
          <Popover open={filterOpen} onOpenChange={(_, data) => setFilterOpen(data.open)} positioning="below-start" withArrow>
            <PopoverTrigger disableButtonEnhancement>
              <ToolbarButton icon={<FilterRegular />} disabled={!canWriteTable}>
                {t("table.filter")}
              </ToolbarButton>
            </PopoverTrigger>
            <PopoverSurface className="view-filter-popover" aria-label={t("table.viewFilters")}>
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
        <DataGrid
          aria-label={t("table.tableRecords")}
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
                disabled={!canWriteTable}
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
                      placeholder="field_score + 1"
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
                  placeholder="field_score + 1"
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
  return field.type !== "formula" && (field.permission_level ?? 2) >= 2;
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
            {relation.field.name} {"->"} {t("table.record")} {relation.row.record_id}
          </Text>
        </div>
        <Button icon={<DismissRegular />} appearance="subtle" aria-label={t("common.close")} onClick={onClose} />
      </div>
      <div className="record-detail-list">
        <label className="field-stack">
          <span>record_id</span>
          <Input aria-label={t("table.valueLabel", { name: "record_id" })} readOnly value={String(relation.row.record_id)} />
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
  canWriteTable,
  field,
  onDeleteField,
  onEditFormula
}: {
  canWriteTable: boolean;
  field: Field;
  onDeleteField: (fieldName: string) => void;
  onEditFormula: (field: Field, point: { x: number; y: number }) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="field-header">
      <span className="field-header-name">{field.name}</span>
      <Menu>
        <MenuTrigger disableButtonEnhancement>
          <button
            type="button"
            className="field-header-menu-button"
            aria-label={t("table.fieldActions", { name: field.name })}
            disabled={!canWriteTable}
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
                onClick={(event) => {
                  const rect = event.currentTarget.getBoundingClientRect();
                  onEditFormula(field, { x: rect.left, y: rect.bottom });
                }}
              >
                {t("table.editFormula")}
              </MenuItem>
            )}
            <MenuItem icon={<DeleteRegular />} onClick={() => onDeleteField(field.name)}>
              {t("table.deleteField")}
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
  const { t } = useTranslation();
  const canEditView = canWriteTable && Boolean(selectedView);
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
      <div className="view-filter-grid">
        <FluentField label={t("table.filterField")}>
          <Select
            aria-label={t("table.viewFilterField")}
            value={newViewFilterField}
            onChange={(_, data) => onNewViewFilterFieldChange(data.value)}
            disabled={!canEditView}
          >
            <option value="">{t("table.noFilter")}</option>
            {activeFields.map((field) => (
              <option key={field.name} value={field.name}>
                {field.name}
              </option>
            ))}
          </Select>
        </FluentField>
        <FluentField label={t("table.filterOperator")}>
          <Select
            aria-label={t("table.viewFilterOperator")}
            value={newViewFilterOp}
            onChange={(_, data) => onNewViewFilterOpChange(data.value as TableViewFilter["op"])}
            disabled={!canEditView || !newViewFilterField}
          >
            <option value="eq">{t("table.operators.eq")}</option>
            <option value="contains">{t("table.operators.contains")}</option>
            <option value="not_empty">{t("table.operators.notEmpty")}</option>
          </Select>
        </FluentField>
      </div>
      <FluentField label={t("table.filterValue")}>
        <Input
          aria-label={t("table.viewFilterValue")}
          value={newViewFilterValue}
          onChange={(_, data) => onNewViewFilterValueChange(data.value)}
          disabled={!canEditView || !newViewFilterField || newViewFilterOp === "not_empty"}
        />
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
            <option value="record_id">record_id</option>
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
          <pre>{JSON.stringify(change.values, null, 2)}</pre>
        </div>
      ))}
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
