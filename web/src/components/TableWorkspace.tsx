import {
  Button,
  Checkbox,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  DialogTrigger,
  Field as FluentField,
  Input,
  Select,
  Text
} from "@fluentui/react-components";
import { AddRegular, DeleteRegular, EditRegular, SaveRegular, TableRegular } from "@fluentui/react-icons";
import DataEditor, {
  type EditableGridCell,
  type GridCell,
  type GridColumn,
  type Item
} from "@glideapps/glide-data-grid";
import type { Field, RowChange, TableMetadata, TableViewFilter, TableViewSort } from "../api";

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
  const canWriteTable = (table.permission_level ?? 2) >= 2;
  const canCreateRow = activeFields.some((field) => canWriteField(field));
  return (
    <div className="table-view">
      <div className="section-header">
        <div>
          <Text weight="semibold">{table.display_name || table.name}</Text>
          <Text size={200}>
            {displayedRows.length} of {rows.length} records
          </Text>
        </div>
        <div className="table-actions">
          <Select
            aria-label="Table view"
            value={selectedTableView}
            onChange={(_, data) => onSelectTableView(data.value)}
          >
            <option value="all">All records</option>
            {(table.views ?? []).map((viewDef) => (
              <option key={viewDef.name} value={viewDef.name}>
                {viewDef.display_name || viewDef.name}
              </option>
            ))}
          </Select>
          <FieldDialog
            activeFields={activeFields}
            canWriteTable={canWriteTable}
            newFieldName={newFieldName}
            newFieldRequired={newFieldRequired}
            newFieldType={newFieldType}
            onAddField={onAddField}
            onDeleteField={onDeleteField}
            onNewFieldNameChange={onNewFieldNameChange}
            onNewFieldRequiredChange={onNewFieldRequiredChange}
            onNewFieldTypeChange={onNewFieldTypeChange}
          />
          <ViewDialog
            activeFields={activeFields}
            canWriteTable={canWriteTable}
            newViewBase={newViewBase}
            newViewFilterField={newViewFilterField}
            newViewFilterOp={newViewFilterOp}
            newViewFilterValue={newViewFilterValue}
            newViewName={newViewName}
            newViewSortDirection={newViewSortDirection}
            newViewSortField={newViewSortField}
            onCreateView={onCreateView}
            onNewViewBaseChange={onNewViewBaseChange}
            onNewViewFilterFieldChange={onNewViewFilterFieldChange}
            onNewViewFilterOpChange={onNewViewFilterOpChange}
            onNewViewFilterValueChange={onNewViewFilterValueChange}
            onNewViewNameChange={onNewViewNameChange}
            onNewViewSortDirectionChange={onNewViewSortDirectionChange}
            onNewViewSortFieldChange={onNewViewSortFieldChange}
            views={table.views ?? []}
          />
          <Select
            aria-label="History record"
            value={selectedRecordID ? String(selectedRecordID) : ""}
            onChange={(_, data) => onSelectRecordID(Number(data.value))}
            disabled={displayedRecordIDs.length === 0}
          >
            {displayedRecordIDs.length === 0 ? (
              <option value="">No records</option>
            ) : (
              displayedRecordIDs.map((recordID) => (
                <option key={recordID} value={recordID}>
                  record #{recordID}
                </option>
              ))
            )}
          </Select>
          <Button onClick={onLoadHistory} disabled={!selectedRecordID}>
            History
          </Button>
          <EditRowDialog
            fields={activeFields}
            onChange={onSelectedRowValueChange}
            onSave={onUpdateSelectedRow}
            selectedRecordID={selectedRecordID}
            values={selectedRowDraft}
          />
          <Button icon={<DeleteRegular />} onClick={onDeleteSelectedRow} disabled={!selectedRecordID || !canWriteTable}>
            Delete Row
          </Button>
          <Button icon={<AddRegular />} appearance="primary" onClick={onAddRow} disabled={!canCreateRow}>
            Row
          </Button>
        </div>
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
              onSelectRecordID(recordID);
            }
          }}
          columns={columns}
          rows={displayedRows.length}
          rowMarkers="number"
          smoothScrollX
          smoothScrollY
          width="100%"
          height="100%"
        />
      </div>
      <div className="row-history-panel" aria-label="Row history">
        {rowHistory.length === 0 ? (
          <Text size={200}>No row history loaded</Text>
        ) : (
          rowHistory.map((change) => (
            <div key={change.history_key} className="row-history-entry">
              <div>
                <Text weight="semibold">{change.history_key}</Text>
                <Text size={200}>
                  {[change.operation, new Date(change.timestamp).toLocaleString()].filter(Boolean).join(" · ")}
                </Text>
              </div>
              <pre>{JSON.stringify(change.values, null, 2)}</pre>
            </div>
          ))
        )}
      </div>
    </div>
  );
}

function FieldDialog({
  activeFields,
  canWriteTable,
  newFieldName,
  newFieldRequired,
  newFieldType,
  onAddField,
  onDeleteField,
  onNewFieldNameChange,
  onNewFieldRequiredChange,
  onNewFieldTypeChange
}: {
  activeFields: Field[];
  canWriteTable: boolean;
  newFieldName: string;
  newFieldRequired: boolean;
  newFieldType: string;
  onAddField: () => void;
  onDeleteField: (fieldName: string) => void;
  onNewFieldNameChange: (value: string) => void;
  onNewFieldRequiredChange: (value: boolean) => void;
  onNewFieldTypeChange: (value: string) => void;
}) {
  return (
    <Dialog>
      <DialogTrigger disableButtonEnhancement>
        <Button icon={<TableRegular />} disabled={!canWriteTable}>
          Fields
        </Button>
      </DialogTrigger>
      <DialogSurface>
        <DialogBody>
          <DialogTitle>Fields</DialogTitle>
          <DialogContent className="modal-stack">
            <div className="modal-list">
              {activeFields.map((field) => (
                <div key={field.name} className="field-row">
                  <div>
                    <Text size={200} weight="semibold">
                      {field.name}
                    </Text>
                    <Text size={100}>
                      {field.type}
                      {field.required ? " · required" : ""}
                    </Text>
                  </div>
                  <Button
                    icon={<DeleteRegular />}
                    aria-label={`Delete field ${field.name}`}
                    onClick={() => onDeleteField(field.name)}
                    disabled={!canWriteTable}
                  />
                </div>
              ))}
            </div>
            <FluentField label="New field name">
              <Input
                aria-label="New field name"
                value={newFieldName}
                onChange={(_, data) => onNewFieldNameChange(data.value)}
                disabled={!canWriteTable}
              />
            </FluentField>
            <FluentField label="New field type">
              <Select
                aria-label="New field type"
                value={newFieldType}
                onChange={(_, data) => onNewFieldTypeChange(data.value)}
                disabled={!canWriteTable}
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
              disabled={!canWriteTable}
            />
          </DialogContent>
          <DialogActions>
            <Button appearance="primary" icon={<AddRegular />} onClick={onAddField} disabled={!canWriteTable}>
              Add Field
            </Button>
            <DialogTrigger disableButtonEnhancement>
              <Button>Close</Button>
            </DialogTrigger>
          </DialogActions>
        </DialogBody>
      </DialogSurface>
    </Dialog>
  );
}

function ViewDialog({
  activeFields,
  canWriteTable,
  newViewBase,
  newViewFilterField,
  newViewFilterOp,
  newViewFilterValue,
  newViewName,
  newViewSortDirection,
  newViewSortField,
  onCreateView,
  onNewViewBaseChange,
  onNewViewFilterFieldChange,
  onNewViewFilterOpChange,
  onNewViewFilterValueChange,
  onNewViewNameChange,
  onNewViewSortDirectionChange,
  onNewViewSortFieldChange,
  views
}: {
  activeFields: Field[];
  canWriteTable: boolean;
  newViewBase: string;
  newViewFilterField: string;
  newViewFilterOp: TableViewFilter["op"];
  newViewFilterValue: string;
  newViewName: string;
  newViewSortDirection: TableViewSort["direction"];
  newViewSortField: string;
  onCreateView: () => void;
  onNewViewBaseChange: (value: string) => void;
  onNewViewFilterFieldChange: (value: string) => void;
  onNewViewFilterOpChange: (value: TableViewFilter["op"]) => void;
  onNewViewFilterValueChange: (value: string) => void;
  onNewViewNameChange: (value: string) => void;
  onNewViewSortDirectionChange: (value: TableViewSort["direction"]) => void;
  onNewViewSortFieldChange: (value: string) => void;
  views: NonNullable<TableMetadata["views"]>;
}) {
  return (
    <Dialog>
      <DialogTrigger disableButtonEnhancement>
        <Button icon={<AddRegular />} disabled={!canWriteTable}>
          View
        </Button>
      </DialogTrigger>
      <DialogSurface>
        <DialogBody>
          <DialogTitle>Create view</DialogTitle>
          <DialogContent className="modal-stack">
            <FluentField label="View name">
              <Input
                aria-label="New view name"
                value={newViewName}
                onChange={(_, data) => onNewViewNameChange(data.value)}
                disabled={!canWriteTable}
              />
            </FluentField>
            <FluentField label="Base view">
              <Select
                aria-label="Base view"
                value={newViewBase}
                onChange={(_, data) => onNewViewBaseChange(data.value)}
                disabled={!canWriteTable}
              >
                <option value="all">All records</option>
                {views.map((item) => (
                  <option key={item.name} value={item.name}>
                    {item.display_name || item.name}
                  </option>
                ))}
              </Select>
            </FluentField>
            <FluentField label="Filter field">
              <Select
                aria-label="View filter field"
                value={newViewFilterField}
                onChange={(_, data) => onNewViewFilterFieldChange(data.value)}
                disabled={!canWriteTable}
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
                disabled={!canWriteTable || !newViewFilterField}
              >
                <option value="eq">equals</option>
                <option value="contains">contains</option>
                <option value="not_empty">not empty</option>
              </Select>
            </FluentField>
            <FluentField label="Filter value">
              <Input
                aria-label="View filter value"
                value={newViewFilterValue}
                onChange={(_, data) => onNewViewFilterValueChange(data.value)}
                disabled={!canWriteTable || !newViewFilterField || newViewFilterOp === "not_empty"}
              />
            </FluentField>
            <FluentField label="Sort field">
              <Select
                aria-label="View sort field"
                value={newViewSortField}
                onChange={(_, data) => onNewViewSortFieldChange(data.value)}
                disabled={!canWriteTable}
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
                disabled={!canWriteTable || !newViewSortField}
              >
                <option value="asc">ascending</option>
                <option value="desc">descending</option>
              </Select>
            </FluentField>
          </DialogContent>
          <DialogActions>
            <Button appearance="primary" icon={<AddRegular />} onClick={onCreateView} disabled={!canWriteTable}>
              Create View
            </Button>
            <DialogTrigger disableButtonEnhancement>
              <Button>Close</Button>
            </DialogTrigger>
          </DialogActions>
        </DialogBody>
      </DialogSurface>
    </Dialog>
  );
}

function EditRowDialog({
  fields,
  onChange,
  onSave,
  selectedRecordID,
  values
}: {
  fields: Field[];
  onChange: (fieldName: string, value: string) => void;
  onSave: () => void;
  selectedRecordID: number;
  values: Record<string, string>;
}) {
  const writableFields = fields.filter(canWriteField);
  const hasWritableFields = writableFields.length > 0;
  return (
    <Dialog>
      <DialogTrigger disableButtonEnhancement>
        <Button icon={<EditRegular />} disabled={!selectedRecordID || !hasWritableFields}>
          Edit Row
        </Button>
      </DialogTrigger>
      <DialogSurface>
        <DialogBody>
          <DialogTitle>Edit row</DialogTitle>
          <DialogContent className="modal-stack">
            <Text size={200}>{selectedRecordID ? `record #${selectedRecordID}` : "No record selected"}</Text>
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
          </DialogContent>
          <DialogActions>
            <Button appearance="primary" icon={<SaveRegular />} onClick={onSave} disabled={!selectedRecordID || !hasWritableFields}>
              Save Row
            </Button>
            <DialogTrigger disableButtonEnhancement>
              <Button>Close</Button>
            </DialogTrigger>
          </DialogActions>
        </DialogBody>
      </DialogSurface>
    </Dialog>
  );
}

function canWriteField(field: Field): boolean {
  return (field.permission_level ?? 2) >= 2;
}
