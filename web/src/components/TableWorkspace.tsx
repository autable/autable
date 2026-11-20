import {
  ArrowDown,
  ArrowUp,
  ArrowUpDown,
  Columns3,
  Eye,
  EyeOff,
  Filter,
  GripVertical,
  History,
  MoreHorizontal,
  Pencil,
  Plus,
  Search,
  Table2,
  Trash2,
  X
} from "lucide-react";
import { type CellSelectArgs, type Column, type RowsChangeData } from "react-data-grid";
import { useEffect, useMemo, useRef, useState, type ComponentProps, type ReactNode } from "react";
import { QueryBuilder, type Field as QueryBuilderField } from "react-querybuilder";
import { useTranslation } from "react-i18next";
import type { Field, RowChange, TableMetadata, TableViewQuery, TableViewSort } from "../api";
import { fieldCreatable, fieldEditable } from "../fieldPermissions";
import { RouteLink } from "../shell/navigation";
import type { TableGridRow } from "../tableGrid";
import {
  Button,
  Checkbox,
  cn,
  Dialog,
  DialogClose,
  DialogContent,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  Field as FormField,
  Input,
  NativeSelect,
  Popover,
  PopoverAnchor,
  PopoverContent,
  PopoverTrigger,
  Tabs,
  TabsList,
  TabsTrigger,
  Textarea
} from "../ui";
import { CreateNamePopover } from "./CreateNamePopover";
import { EnumOptionCheckboxes } from "./EnumOptionCheckboxes";
import { ExcelImportButton } from "./ExcelImportButton";
import { RecordDataGrid } from "./RecordDataGrid";

type TableWorkspaceProps = {
  columns: Column<TableGridRow>[];
  databaseName: string;
  displayedRows: TableGridRow[];
  // Only admins change structure: fields, formulas, views.
  isAdmin: boolean;
  openViewPanelRequest: number;
  onAddRow: () => void;
  onAddField: () => void;
  onCreateView: (viewName: string) => void;
  onRowsChange: (rows: TableGridRow[], data: RowsChangeData<TableGridRow>) => void | Promise<void>;
  onDeleteField: (fieldName: string) => void;
  onDeleteSelectedRow: (recordID?: number) => void;
  onLoadHistory: () => void;
  onNewFieldFormulaChange: (value: string) => void;
  onNewFieldNameChange: (value: string) => void;
  onNewFieldOptionsChange: (value: string) => void;
  onNewFieldMultipleChange: (value: boolean) => void;
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
  onUpdateFieldOptions: (fieldName: string, optionsText: string, multiple: boolean) => void;
  onImported: () => void | Promise<void>;
  onOpenRelationCell: (fieldName: string, row: TableGridRow) => void;
  newFieldFormula: string;
  newFieldName: string;
  newFieldMultiple: boolean;
  newFieldOptions: string;
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

type Point = { x: number; y: number };

// A zero-size anchor at a screen point, for panels opened from a grid header
// or a context menu rather than from a trigger of their own.
function useVirtualAnchor(point: Point | null) {
  return useMemo(
    () => ({
      current: {
        getBoundingClientRect: () => DOMRect.fromRect({ x: point?.x ?? 0, y: point?.y ?? 0, width: 0, height: 0 })
      }
    }),
    [point?.x, point?.y]
  );
}

export function TableWorkspace({
  columns,
  databaseName,
  displayedRows,
  isAdmin,
  openViewPanelRequest,
  onAddRow,
  onAddField,
  onCreateView,
  onRowsChange,
  onDeleteField,
  onDeleteSelectedRow,
  onLoadHistory,
  onNewFieldFormulaChange,
  onNewFieldNameChange,
  onNewFieldOptionsChange,
  onNewFieldMultipleChange,
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
  onUpdateFieldOptions,
  onImported,
  onOpenRelationCell,
  newFieldFormula,
  newFieldName,
  newFieldMultiple,
  newFieldOptions,
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
  const canWriteFields = hasTable && isAdmin;
  const canCreateRow = selectedTableView === "all" && activeFields.some((field) => canCreateField(field));
  const hasWritableFields = activeFields.some(canWriteField);
  const [recordPanelOpen, setRecordPanelOpen] = useState(false);
  const [recordPanelTab, setRecordPanelTab] = useState<"details" | "history">("details");
  const [filterOpen, setFilterOpen] = useState(false);
  const [fieldSettingsOpen, setFieldSettingsOpen] = useState(false);
  const [newViewName, setNewViewName] = useState("");
  const [fieldVisibilityState, setFieldVisibilityState] = useState<{ key: string; hiddenFieldNames: string[] }>({
    key: "",
    hiddenFieldNames: []
  });
  const [recordMenu, setRecordMenu] = useState<(Point & { recordID: number }) | null>(null);
  const [fieldCreator, setFieldCreator] = useState<Point | null>(null);
  const [formulaEditor, setFormulaEditor] = useState<(Point & { fieldName: string; formula: string }) | null>(null);
  const [optionsEditor, setOptionsEditor] = useState<(Point & { fieldName: string; options: string; multiple: boolean }) | null>(null);
  const fieldCreatorAnchor = useVirtualAnchor(fieldCreator);
  const formulaEditorAnchor = useVirtualAnchor(formulaEditor);
  const optionsEditorAnchor = useVirtualAnchor(optionsEditor);
  const selectedView = useMemo(
    () => (table.views ?? []).find((viewDef) => viewDef.name === selectedTableView),
    [selectedTableView, table.views]
  );
  const canWriteSelectedView = hasTable && isAdmin;
  const activeFieldNameKey = useMemo(() => activeFields.map((field) => field.name).join("\u0000"), [activeFields]);
  const fieldVisibilityStorageKey = useMemo(
    () => createFieldVisibilityStorageKey(databaseName, table.name, selectedTableView),
    [databaseName, table.name, selectedTableView]
  );
  const hiddenFieldNames = fieldVisibilityState.key === fieldVisibilityStorageKey ? fieldVisibilityState.hiddenFieldNames : [];
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
      const currentHidden =
        current.key === fieldVisibilityStorageKey ? current.hiddenFieldNames : readHiddenFieldNames(fieldVisibilityStorageKey, activeFieldNames);
      const nextHidden = currentHidden.includes(fieldName)
        ? currentHidden.filter((name) => name !== fieldName)
        : [...currentHidden, fieldName];
      return {
        key: fieldVisibilityStorageKey,
        hiddenFieldNames: nextHidden.filter((name) => activeFieldNames.includes(name))
      };
    });
  }

  const gridColumns = useMemo(() => {
    const fieldColumns = columns
      .filter((column) => !hiddenFieldNameSet.has(String(column.key)))
      .map((column) => {
        const field = activeFields.find((item) => item.name === column.key);
        if (!field) {
          return column;
        }
        return {
          ...column,
          draggable: canWriteFields,
          renderHeaderCell: () => (
            <FieldHeader
              canWriteFields={canWriteFields}
              field={field}
              onDeleteField={onDeleteField}
              onEditFormula={(targetField, point) =>
                setFormulaEditor({ ...point, fieldName: targetField.name, formula: targetField.formula ?? "" })
              }
              onEditOptions={(targetField, point) =>
                setOptionsEditor({
                  ...point,
                  fieldName: targetField.name,
                  options: (targetField.options ?? []).join(", "),
                  multiple: Boolean(targetField.multiple)
                })
              }
              onSort={(fieldName, direction) => onTemporarySortChange(direction ? { field: fieldName, direction } : undefined)}
              sortDirection={temporarySort?.field === field.name ? temporarySort.direction : undefined}
            />
          )
        } satisfies Column<TableGridRow>;
      });
    const addFieldColumn: Column<TableGridRow> = {
      key: "__add_field__",
      name: "",
      width: 44,
      minWidth: 44,
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
          <Plus aria-hidden />
        </button>
      ),
      renderCell: () => ""
    };
    return canWriteFields ? [...fieldColumns, addFieldColumn] : fieldColumns;
  }, [
    activeFields,
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
  ]);

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

  const title = table.display_name || table.name;
  return (
    <div className="table-view flex h-full min-h-0 flex-col">
      <div className="shrink-0 px-4 pt-4 md:px-8 md:pt-7">
        <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-3">
          <div className="flex min-w-0 items-center gap-3">
            <span className="hidden size-10 shrink-0 items-center justify-center rounded-inner bg-chip text-description sm:flex" aria-hidden>
              <Table2 className="size-5" strokeWidth={1.75} />
            </span>
            <div className="min-w-0">
              <h1 className="truncate text-20 font-medium tracking-tight text-fg">{title}</h1>
              <p className="text-13 tabular-nums text-muted">{t("table.recordCount", { shown: displayedRows.length, total: totalRows })}</p>
            </div>
          </div>
          <div
            role="toolbar"
            aria-label={t("table.tableActions")}
            className="flex w-full flex-wrap items-center gap-1 sm:w-auto sm:flex-nowrap"
          >
            <label className="relative mr-1 block w-full sm:w-56">
              <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-subtle" aria-hidden />
              <Input
                type="search"
                inputSize="sm"
                className="pl-9"
                aria-label={t("table.searchRecords")}
                placeholder={t("table.searchPlaceholder")}
                value={searchText}
                onChange={(event) => onSearchTextChange(event.target.value)}
              />
            </label>
            {canWriteSelectedView && (
              <Popover open={filterOpen} onOpenChange={setFilterOpen}>
                <PopoverTrigger asChild>
                  <ToolbarButton icon={<Filter />} label={t("table.filter")} />
                </PopoverTrigger>
                <PopoverContent align="end" aria-label={t("table.viewFilters")} className="w-[min(40rem,calc(100vw-1rem))] p-0">
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
                </PopoverContent>
              </Popover>
            )}
            <ToolbarButton icon={<Columns3 />} label={t("table.fields")} onClick={() => setFieldSettingsOpen(true)} />
            <ToolbarButton
              icon={<Pencil />}
              label={t("table.editRow")}
              onClick={() => openRecordPanel()}
              disabled={!selectedRecordID || !hasWritableFields}
            />
            <ToolbarButton icon={<History />} label={t("common.history")} onClick={() => openHistoryPanel()} disabled={!selectedRecordID} />
            <ExcelImportButton
              databaseName={databaseName}
              tableName={table.name}
              tables={tables}
              disabled={!canCreateRow}
              onImported={() => void onImported()}
            />
            <Button variant="cta" size="sm" className="ml-1 pl-3" onClick={onAddRow} disabled={!canCreateRow}>
              <Plus aria-hidden />
              {t("table.row")}
            </Button>
          </div>
        </div>
        <ViewStrip
          databaseName={databaseName}
          isAdmin={isAdmin}
          newViewName={newViewName}
          onCreateView={() => {
            onCreateView(newViewName);
            setNewViewName("");
          }}
          onNewViewNameChange={setNewViewName}
          selectedTableView={selectedTableView}
          table={table}
        />
      </div>
      <div className="grid-host relative mx-4 mb-4 mt-3 flex min-h-0 flex-1 overflow-hidden rounded-container border border-border bg-elevated md:mx-8 md:mb-6">
        <div className="flex min-w-0 flex-1">
          <RecordDataGrid
            aria-label={t("table.tableRecords")}
            className="flex-1"
            columns={gridColumns}
            rows={displayedRows}
            rowKeyGetter={(row) => row.ct_record_id}
            onRowsChange={(nextRows, data) => {
              void onRowsChange(nextRows, data);
            }}
            onSelectedCellChange={(args) => {
              onSelectGridCell(args);
            }}
            onCellDoubleClick={({ column, row }) => {
              onOpenRelationCell(String(column.key), row);
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
        </div>
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
        {relationDetail && <RelationDrawer relation={relationDetail} onClose={onCloseRelationDetail} />}
      </div>

      <Dialog open={fieldSettingsOpen} onOpenChange={setFieldSettingsOpen}>
        <DialogContent
          title={t("table.fieldSettings")}
          className="max-w-lg"
          footer={
            <DialogClose asChild>
              <Button>{t("common.close")}</Button>
            </DialogClose>
          }
        >
          <FieldSettingsList
            canWriteFields={canWriteFields}
            fields={activeFields}
            hiddenFieldNames={hiddenFieldNames}
            onMoveFieldPosition={onMoveFieldPosition}
            onToggleFieldVisibility={toggleFieldVisibility}
          />
        </DialogContent>
      </Dialog>

      <DropdownMenu
        open={Boolean(recordMenu)}
        onOpenChange={(open) => {
          if (!open) {
            setRecordMenu(null);
          }
        }}
      >
        <DropdownMenuTrigger asChild>
          <span aria-hidden className="pointer-events-none fixed size-0" style={{ left: recordMenu?.x ?? 0, top: recordMenu?.y ?? 0 }} />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" sideOffset={2}>
          <DropdownMenuItem onSelect={() => recordMenu && openRecordPanel(recordMenu.recordID)}>
            <Pencil />
            {t("table.viewDetails")}
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={() => recordMenu && openHistoryPanel(recordMenu.recordID)}>
            <History />
            {t("table.viewHistory")}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem danger onSelect={() => recordMenu && onDeleteSelectedRow(recordMenu.recordID)}>
            <Trash2 />
            {t("table.deleteRecord")}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <Popover open={Boolean(fieldCreator)} onOpenChange={(open) => !open && setFieldCreator(null)}>
        <PopoverAnchor virtualRef={fieldCreatorAnchor} />
        <PopoverContent aria-label={t("table.addField")} className="w-80 p-4">
          <FieldEditorForm
            title={t("table.addField")}
            onCancel={() => setFieldCreator(null)}
            onSubmit={() => {
              void onAddField();
              setFieldCreator(null);
            }}
            submitLabel={t("common.add")}
          >
            <FormField label={t("table.fieldName")}>
              {(field) => (
                <Input
                  {...field}
                  autoFocus
                  inputSize="sm"
                  aria-label={t("table.fieldName")}
                  value={newFieldName}
                  onChange={(event) => onNewFieldNameChange(event.target.value)}
                />
              )}
            </FormField>
            <FormField label={t("table.fieldType")}>
              {(field) => (
                <NativeSelect
                  {...field}
                  selectSize="sm"
                  aria-label={t("table.fieldType")}
                  value={newFieldType}
                  onChange={(event) => {
                    const value = event.target.value;
                    onNewFieldTypeChange(value);
                    if (value === "relation" && !newRelationTable) {
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
                </NativeSelect>
              )}
            </FormField>
            {newFieldType === "string" && (
              <FormField label={t("table.fieldOptions")}>
                {(field) => (
                  <Input
                    {...field}
                    inputSize="sm"
                    aria-label={t("table.fieldOptions")}
                    value={newFieldOptions}
                    onChange={(event) => onNewFieldOptionsChange(event.target.value)}
                    placeholder={t("table.fieldOptionsPlaceholder")}
                  />
                )}
              </FormField>
            )}
            {newFieldType === "string" && newFieldOptions.trim() !== "" && (
              <Checkbox
                checked={newFieldMultiple}
                label={t("table.fieldOptionsMultiple")}
                onChange={(event) => onNewFieldMultipleChange(event.target.checked)}
              />
            )}
            {newFieldType === "relation" && (
              <FormField label={t("table.targetTable")}>
                {(field) => (
                  <NativeSelect
                    {...field}
                    selectSize="sm"
                    aria-label={t("table.targetTable")}
                    value={newRelationTable}
                    onChange={(event) => onNewRelationTableChange(event.target.value)}
                  >
                    {tables.map((item) => (
                      <option key={item.name} value={item.name}>
                        {item.display_name || item.name}
                      </option>
                    ))}
                  </NativeSelect>
                )}
              </FormField>
            )}
            {newFieldType === "formula" && (
              <>
                <FormField label={t("table.formulaValueType")}>
                  {(field) => (
                    <NativeSelect
                      {...field}
                      selectSize="sm"
                      aria-label={t("table.formulaValueType")}
                      value={newFormulaValueType}
                      onChange={(event) => onNewFormulaValueTypeChange(event.target.value)}
                    >
                      <option value="string">string</option>
                      <option value="int">int</option>
                      <option value="float">float</option>
                    </NativeSelect>
                  )}
                </FormField>
                <FormField label={t("table.formula")}>
                  {(field) => (
                    <Textarea
                      {...field}
                      aria-label={t("table.formula")}
                      className="font-mono text-13"
                      value={newFieldFormula}
                      onChange={(event) => onNewFieldFormulaChange(event.target.value)}
                      placeholder={'fields["score"] + 1'}
                    />
                  )}
                </FormField>
              </>
            )}
          </FieldEditorForm>
        </PopoverContent>
      </Popover>

      <Popover open={Boolean(formulaEditor)} onOpenChange={(open) => !open && setFormulaEditor(null)}>
        <PopoverAnchor virtualRef={formulaEditorAnchor} />
        <PopoverContent aria-label={t("table.editFormula")} className="w-96 p-4">
          <FieldEditorForm
            title={formulaEditor?.fieldName ?? ""}
            onCancel={() => setFormulaEditor(null)}
            onSubmit={() => {
              if (formulaEditor) {
                void onUpdateFieldFormula(formulaEditor.fieldName, formulaEditor.formula);
              }
              setFormulaEditor(null);
            }}
            submitLabel={t("common.save")}
          >
            <FormField label={t("table.formula")}>
              {(field) => (
                <Textarea
                  {...field}
                  autoFocus
                  aria-label={t("table.fieldFormula")}
                  className="font-mono text-13"
                  value={formulaEditor?.formula ?? ""}
                  onChange={(event) => {
                    const formula = event.target.value;
                    setFormulaEditor((current) => (current ? { ...current, formula } : current));
                  }}
                  placeholder={'fields["score"] + 1'}
                />
              )}
            </FormField>
          </FieldEditorForm>
        </PopoverContent>
      </Popover>

      <Popover open={Boolean(optionsEditor)} onOpenChange={(open) => !open && setOptionsEditor(null)}>
        <PopoverAnchor virtualRef={optionsEditorAnchor} />
        <PopoverContent aria-label={t("table.editOptions")} className="w-80 p-4">
          <FieldEditorForm
            title={optionsEditor?.fieldName ?? ""}
            onCancel={() => setOptionsEditor(null)}
            onSubmit={() => {
              if (optionsEditor) {
                void onUpdateFieldOptions(optionsEditor.fieldName, optionsEditor.options, optionsEditor.multiple);
              }
              setOptionsEditor(null);
            }}
            submitLabel={t("common.save")}
          >
            <FormField label={t("table.fieldOptions")}>
              {(field) => (
                <Input
                  {...field}
                  autoFocus
                  inputSize="sm"
                  aria-label={t("table.fieldOptions")}
                  value={optionsEditor?.options ?? ""}
                  onChange={(event) => {
                    const options = event.target.value;
                    setOptionsEditor((current) => (current ? { ...current, options } : current));
                  }}
                  placeholder={t("table.fieldOptionsPlaceholder")}
                />
              )}
            </FormField>
            <Checkbox
              checked={Boolean(optionsEditor?.multiple)}
              label={t("table.fieldOptionsMultiple")}
              onChange={(event) => {
                const multiple = event.target.checked;
                setOptionsEditor((current) => (current ? { ...current, multiple } : current));
              }}
            />
          </FieldEditorForm>
        </PopoverContent>
      </Popover>
    </div>
  );
}

/** Toolbar action: icon + label, icon-only below the sm breakpoint (the label stays its accessible name). */
function ToolbarButton({
  icon,
  label,
  className,
  ...props
}: { icon: ReactNode; label: string } & Omit<ComponentProps<typeof Button>, "children">) {
  return (
    <Button variant="quiet" size="sm" aria-label={label} className={cn("px-2 sm:px-2.5", className)} {...props}>
      {icon}
      <span className="hidden lg:inline">{label}</span>
    </Button>
  );
}

/** The view selector: one pill per view the user can read, plus + for admins. */
function ViewStrip({
  databaseName,
  isAdmin,
  newViewName,
  onCreateView,
  onNewViewNameChange,
  selectedTableView,
  table
}: {
  databaseName: string;
  isAdmin: boolean;
  newViewName: string;
  onCreateView: () => void;
  onNewViewNameChange: (value: string) => void;
  selectedTableView: string;
  table: TableMetadata;
}) {
  const { t } = useTranslation();
  return (
    <nav aria-label={t("table.views")} className="-mx-1 mt-4 flex items-center gap-1 overflow-x-auto px-1 pb-0.5">
      {(table.views ?? []).map((viewDef) => {
        const active = viewDef.name === selectedTableView;
        return (
          <RouteLink
            key={viewDef.name}
            route={{ databaseName, view: "table", tableName: table.name, tableViewName: viewDef.name }}
            aria-current={active ? "page" : undefined}
            className={cn(
              "inline-flex h-7 shrink-0 items-center rounded-full px-3 text-13 font-medium transition-colors duration-150",
              active ? "bg-fg text-surface" : "text-muted hover:bg-hover hover:text-fg"
            )}
          >
            {viewDef.display_name || (viewDef.name === "all" ? t("common.allRecords") : viewDef.name)}
          </RouteLink>
        );
      })}
      {isAdmin && (
        <CreateNamePopover
          title={t("nav.newViewTitle")}
          inputLabel={t("nav.newViewName")}
          placeholder={t("nav.placeholderView")}
          name={newViewName}
          onNameChange={onNewViewNameChange}
          onSave={onCreateView}
          trigger={
            <Button variant="quiet" size="icon-sm" aria-label={t("nav.createView")}>
              <Plus />
            </Button>
          }
        />
      )}
    </nav>
  );
}

function FieldEditorForm({
  title,
  children,
  onCancel,
  onSubmit,
  submitLabel
}: {
  title: string;
  children: ReactNode;
  onCancel: () => void;
  onSubmit: () => void;
  submitLabel: string;
}) {
  const { t } = useTranslation();
  return (
    <form
      className="flex flex-col gap-3"
      onSubmit={(event) => {
        event.preventDefault();
        onSubmit();
      }}
    >
      <p className="truncate text-14 font-medium text-fg">{title}</p>
      {children}
      <div className="flex justify-end gap-2 pt-1">
        <Button size="sm" onClick={onCancel}>
          {t("common.cancel")}
        </Button>
        <Button size="sm" variant="cta" type="submit">
          {submitLabel}
        </Button>
      </div>
    </form>
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
    <div className="flex flex-col divide-y divide-border-soft rounded-container border border-border" role="list">
      {fields.map((field) => {
        const hidden = hiddenFieldNameSet.has(field.name);
        return (
          <div
            className={cn("flex items-center gap-2 px-2 py-1.5", draggedFieldName === field.name && "opacity-50")}
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
            {canWriteFields ? (
              <button
                type="button"
                className="flex size-7 shrink-0 cursor-grab items-center justify-center rounded-inner text-subtle hover:bg-hover hover:text-fg active:cursor-grabbing"
                aria-label={t("table.dragField", { name: field.name })}
                draggable
                onDragStart={(event) => {
                  setDraggedFieldName(field.name);
                  event.dataTransfer.effectAllowed = "move";
                  event.dataTransfer.setData("text/plain", field.name);
                }}
                onDragEnd={() => setDraggedFieldName("")}
              >
                <GripVertical className="size-4" aria-hidden />
              </button>
            ) : (
              <span className="w-1" aria-hidden />
            )}
            <div className="min-w-0 flex-1">
              <p className={cn("truncate text-13 font-medium", hidden ? "text-muted" : "text-fg")}>{field.name}</p>
              <p className="text-12 text-subtle">{field.type}</p>
            </div>
            <Button
              variant="quiet"
              size="icon-sm"
              aria-label={hidden ? t("table.showField", { name: field.name }) : t("table.hideField", { name: field.name })}
              aria-pressed={!hidden}
              onClick={() => onToggleFieldVisibility(field.name)}
            >
              {hidden ? <EyeOff /> : <Eye />}
            </Button>
          </div>
        );
      })}
    </div>
  );
}

function SidePanel({
  label,
  title,
  subtitle,
  onClose,
  children,
  footer
}: {
  label: string;
  title: ReactNode;
  subtitle: ReactNode;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
}) {
  const { t } = useTranslation();
  return (
    <aside
      aria-label={label}
      className="absolute inset-y-0 right-0 z-10 flex w-full max-w-[22rem] flex-col border-l border-border bg-elevated shadow-float animate-fade-in lg:static lg:z-auto lg:w-[22rem] lg:shrink-0 lg:shadow-none"
    >
      <div className="flex shrink-0 items-start justify-between gap-3 px-5 pt-4">
        <div className="min-w-0">
          <p className="truncate text-15 font-medium text-fg">{title}</p>
          <p className="truncate text-12 text-muted">{subtitle}</p>
        </div>
        <Button variant="quiet" size="icon-sm" aria-label={t("common.close")} className="-mr-1.5" onClick={onClose}>
          <X />
        </Button>
      </div>
      {children}
      {footer && <div className="flex shrink-0 justify-end gap-2 border-t border-border-soft px-5 py-3">{footer}</div>}
    </aside>
  );
}

function RelationDrawer({ relation, onClose }: { relation: { field: Field; table: TableMetadata; row: TableGridRow }; onClose: () => void }) {
  const { t } = useTranslation();
  const fields = relation.table.fields.filter((field) => !field.deleted);
  return (
    <SidePanel
      label={t("table.relationRecordDetail")}
      title={relation.table.display_name || relation.table.name}
      subtitle={`${relation.field.name} -> ${t("table.record")} ${relation.row.ct_record_id}`}
      onClose={onClose}
    >
      <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto px-5 py-4">
        <ReadOnlyValue name="ct_record_id" value={String(relation.row.ct_record_id)} />
        {fields.map((field) => (
          <ReadOnlyValue key={field.name} name={field.name} value={String(relation.row[field.name] ?? "")} />
        ))}
      </div>
    </SidePanel>
  );
}

function ReadOnlyValue({ name, value }: { name: string; value: string }) {
  const { t } = useTranslation();
  return (
    <FormField label={name}>
      {(field) => <Input {...field} inputSize="sm" aria-label={t("table.valueLabel", { name })} readOnly value={value} />}
    </FormField>
  );
}

function FieldHeader({
  canWriteFields,
  field,
  onDeleteField,
  onEditFormula,
  onEditOptions,
  onSort,
  sortDirection
}: {
  canWriteFields: boolean;
  field: Field;
  onDeleteField: (fieldName: string) => void;
  onEditFormula: (field: Field, point: Point) => void;
  onEditOptions: (field: Field, point: Point) => void;
  onSort: (fieldName: string, direction?: TableViewSort["direction"]) => void;
  sortDirection?: TableViewSort["direction"];
}) {
  const { t } = useTranslation();
  // Set when a menu entry opens an editor, so the closing menu does not pull
  // focus back to its trigger and dismiss the editor that just opened.
  const openingEditor = useRef(false);
  const nextSortDirection = sortDirection === "desc" ? "asc" : sortDirection === "asc" ? undefined : "desc";
  const SortIcon = sortDirection === "asc" ? ArrowUp : sortDirection === "desc" ? ArrowDown : ArrowUpDown;
  function editorPoint(event: Event): Point {
    const rect = (event.target as HTMLElement).getBoundingClientRect();
    return { x: rect.left, y: rect.bottom };
  }
  return (
    <div className="field-header">
      <span className="field-header-name" title={field.name}>
        {field.name}
      </span>
      <button
        type="button"
        className={cn("field-header-button", sortDirection && "is-active")}
        aria-label={t("table.toggleSort", { name: field.name })}
        aria-pressed={Boolean(sortDirection)}
        title={t("table.toggleSort", { name: field.name })}
        onClick={(event) => {
          event.preventDefault();
          event.stopPropagation();
          onSort(field.name, nextSortDirection);
        }}
      >
        <SortIcon aria-hidden />
      </button>
      {canWriteFields && (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button
              type="button"
              className="field-header-button field-header-menu"
              aria-label={t("table.fieldActions", { name: field.name })}
              onClick={(event) => event.stopPropagation()}
            >
              <MoreHorizontal aria-hidden />
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent
            align="end"
            onCloseAutoFocus={(event) => {
              if (openingEditor.current) {
                event.preventDefault();
                openingEditor.current = false;
              }
            }}
          >
            {field.type === "formula" && (
              <DropdownMenuItem
                onSelect={(event) => {
                  openingEditor.current = true;
                  onEditFormula(field, editorPoint(event));
                }}
              >
                <Pencil />
                {t("table.editFormula")}
              </DropdownMenuItem>
            )}
            {field.type === "string" && (
              <DropdownMenuItem
                onSelect={(event) => {
                  openingEditor.current = true;
                  onEditOptions(field, editorPoint(event));
                }}
              >
                <Pencil />
                {t("table.editOptions")}
              </DropdownMenuItem>
            )}
            <DropdownMenuItem danger onSelect={() => onDeleteField(field.name)}>
              <Trash2 />
              {t("table.deleteField")}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      )}
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
        inputType:
          field.type === "int" || field.type === "float" || field.value_type === "int" || field.value_type === "float" ? "number" : "text"
      }))
    ],
    [activeFields]
  );
  return (
    <div className="flex flex-col">
      <div className="border-b border-border-soft px-5 pb-3 pt-4">
        <p className="text-15 font-medium text-fg">{selectedView?.display_name || selectedView?.name || t("common.allRecords")}</p>
        <p className="text-12 text-muted">
          {selectedView?.base_view
            ? t("table.basedOn", { view: selectedView.base_view })
            : selectedView
              ? t("table.tableView")
              : t("common.baseTable")}
        </p>
      </div>
      <div className="flex flex-col gap-4 px-5 py-4">
        <FormField label={t("table.baseView")}>
          {(field) => (
            <NativeSelect
              {...field}
              selectSize="sm"
              aria-label={t("table.baseView")}
              value={newViewBase}
              onChange={(event) => onNewViewBaseChange(event.target.value)}
              disabled={!canEditView}
            >
              <option value="all">{t("common.allRecords")}</option>
              {views
                .filter((item) => item.name !== selectedView?.name && item.name !== "all")
                .map((item) => (
                  <option key={item.name} value={item.name}>
                    {item.display_name || item.name}
                  </option>
                ))}
            </NativeSelect>
          )}
        </FormField>
        <div className="flex flex-col gap-1.5">
          <p className="text-13 font-medium text-fg">{t("table.viewFilters")}</p>
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
                removeRule: { label: "×", title: t("common.delete") },
                removeGroup: { label: "×", title: t("common.delete") }
              }}
            />
          </div>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <FormField label={t("table.sortField")}>
            {(field) => (
              <NativeSelect
                {...field}
                selectSize="sm"
                aria-label={t("table.viewSortField")}
                value={newViewSortField}
                onChange={(event) => onNewViewSortFieldChange(event.target.value)}
                disabled={!canEditView}
              >
                <option value="">{t("table.noSort")}</option>
                <option value="ct_record_id">ct_record_id</option>
                {activeFields.map((field) => (
                  <option key={field.name} value={field.name}>
                    {field.name}
                  </option>
                ))}
              </NativeSelect>
            )}
          </FormField>
          <FormField label={t("table.sortDirection")}>
            {(field) => (
              <NativeSelect
                {...field}
                selectSize="sm"
                aria-label={t("table.viewSortDirection")}
                value={newViewSortDirection}
                onChange={(event) => onNewViewSortDirectionChange(event.target.value as TableViewSort["direction"])}
                disabled={!canEditView || !newViewSortField}
              >
                <option value="asc">{t("table.ascending")}</option>
                <option value="desc">{t("table.descending")}</option>
              </NativeSelect>
            )}
          </FormField>
        </div>
      </div>
      <div className="flex justify-end border-t border-border-soft px-5 py-3">
        <Button variant="cta" size="sm" onClick={onSaveView} disabled={!canEditView}>
          {t("table.saveView")}
        </Button>
      </div>
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
    <SidePanel
      label={t("table.recordPanel")}
      title={selectedRecordID ? t("table.recordTitle", { id: selectedRecordID }) : t("table.noRecordSelected")}
      subtitle={hasWritableFields ? t("table.writableFields") : t("table.readOnly")}
      onClose={onClose}
      footer={
        tab === "details" ? (
          <Button variant="cta" size="sm" onClick={onSave} disabled={!selectedRecordID || !hasWritableFields}>
            {t("table.saveRow")}
          </Button>
        ) : (
          <Button size="sm" onClick={onLoadHistory} disabled={!selectedRecordID}>
            {t("common.refresh")}
          </Button>
        )
      }
    >
      <Tabs value={tab} onValueChange={(value) => onTabChange(value as "details" | "history")} className="shrink-0 px-5 pt-3">
        <TabsList aria-label={t("table.recordTabs")}>
          <TabsTrigger value="details">{t("common.details")}</TabsTrigger>
          <TabsTrigger value="history">{t("common.history")}</TabsTrigger>
        </TabsList>
      </Tabs>
      {tab === "details" ? (
        <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-5 py-4">
          {fields.map((field) => (
            <FormField key={field.name} label={field.name}>
              {(control) =>
                field.type === "string" && field.options?.length && field.multiple ? (
                  <EnumOptionCheckboxes
                    label={t("table.valueLabel", { name: field.name })}
                    options={field.options}
                    value={values[field.name] ?? ""}
                    disabled={!selectedRecordID || !canWriteField(field)}
                    onChange={(next) => onChange(field.name, next)}
                  />
                ) : field.type === "string" && field.options?.length ? (
                  <NativeSelect
                    {...control}
                    selectSize="sm"
                    aria-label={t("table.valueLabel", { name: field.name })}
                    value={values[field.name] ?? ""}
                    onChange={(event) => onChange(field.name, event.target.value)}
                    disabled={!selectedRecordID || !canWriteField(field)}
                  >
                    <option value="" />
                    {field.options.map((option) => (
                      <option key={option} value={option}>
                        {option}
                      </option>
                    ))}
                  </NativeSelect>
                ) : (
                  <Input
                    {...control}
                    inputSize="sm"
                    aria-label={t("table.valueLabel", { name: field.name })}
                    value={values[field.name] ?? ""}
                    onChange={(event) => onChange(field.name, event.target.value)}
                    disabled={!selectedRecordID || !canWriteField(field)}
                  />
                )
              }
            </FormField>
          ))}
        </div>
      ) : (
        <div className="flex min-h-0 flex-1 flex-col overflow-y-auto px-5 py-4" aria-label={t("table.rowHistory")} role="region">
          <RowHistoryList rowHistory={rowHistory} />
        </div>
      )}
    </SidePanel>
  );
}

function RowHistoryList({ rowHistory }: { rowHistory: RowChange[] }) {
  const { t } = useTranslation();
  if (rowHistory.length === 0) {
    return <p className="py-6 text-center text-13 text-muted">{t("table.noRowHistoryLoaded")}</p>;
  }
  return (
    <ol className="flex flex-col gap-3">
      {rowHistory.map((change) => (
        <li key={change.history_key} className="rounded-container border border-border-soft bg-surface/50 px-3.5 py-3">
          <p className="text-13 font-medium text-fg">{friendlyHistoryOperation(change.operation, t)}</p>
          <p className="text-12 text-muted">
            {[formatHistoryTime(change.timestamp), change.actor_id ? t("table.byActor", { actor: change.actor_id }) : ""]
              .filter(Boolean)
              .join(" · ")}
          </p>
          <HistoryJSON title={t("table.diff")} value={change.diff ?? {}} />
          <HistoryJSON title={t("table.values")} value={change.values} />
        </li>
      ))}
    </ol>
  );
}

function HistoryJSON({ title, value }: { title: string; value: unknown }) {
  return (
    <div className="mt-2.5">
      <p className="text-12 font-medium text-muted">{title}</p>
      <pre className="mt-1 max-h-48 overflow-auto rounded-inner bg-chip px-2.5 py-2 font-mono text-12 leading-relaxed text-description [overflow-wrap:anywhere] whitespace-pre-wrap">
        {JSON.stringify(value, null, 2)}
      </pre>
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
