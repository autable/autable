import {
  Button,
  Checkbox,
  Field as FluentField,
  Input,
  List,
  ListItem,
  Select,
  Text,
  Toolbar,
  ToolbarButton
} from "@fluentui/react-components";
import { AddRegular, SaveRegular } from "@fluentui/react-icons";
import type { Field, RowChange, TableMetadata, TableViewFilter, TableViewSort } from "../api";

export type CanvasPanel = "fields" | "record" | "view" | "history";

type TableCanvasPanelProps = {
  activeFields: Field[];
  activePanel: CanvasPanel;
  canWriteTable: boolean;
  displayedRecordIDs: number[];
  hasWritableFields: boolean;
  newFieldName: string;
  newFieldRequired: boolean;
  newFieldType: string;
  newViewBase: string;
  newViewFilterField: string;
  newViewFilterOp: TableViewFilter["op"];
  newViewFilterValue: string;
  newViewSortDirection: TableViewSort["direction"];
  newViewSortField: string;
  onAddField: () => void;
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
  onOpenHistory: () => void;
  onOpenRecord: () => void;
  onOpenView: () => void;
  onSaveRecord: () => void;
  onSaveView: () => void;
  onSelectField: (fieldName: string) => void;
  onSelectRecordID: (recordID: number) => void;
  onSelectTableView: (name: string) => void;
  onSelectedRowValueChange: (fieldName: string, value: string) => void;
  rowHistory: RowChange[];
  selectedField?: Field;
  selectedRecordID: number;
  selectedRowDraft: Record<string, string>;
  selectedView?: NonNullable<TableMetadata["views"]>[number];
  views: NonNullable<TableMetadata["views"]>;
};

export function TableCanvasPanel({
  activeFields,
  activePanel,
  canWriteTable,
  displayedRecordIDs,
  hasWritableFields,
  newFieldName,
  newFieldRequired,
  newFieldType,
  newViewBase,
  newViewFilterField,
  newViewFilterOp,
  newViewFilterValue,
  newViewSortDirection,
  newViewSortField,
  onAddField,
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
  onOpenHistory,
  onOpenRecord,
  onOpenView,
  onSaveRecord,
  onSaveView,
  onSelectField,
  onSelectRecordID,
  onSelectTableView,
  onSelectedRowValueChange,
  rowHistory,
  selectedField,
  selectedRecordID,
  selectedRowDraft,
  selectedView,
  views
}: TableCanvasPanelProps) {
  return (
    <div className="table-canvas-panel" aria-label="Table canvas panel">
      <CanvasPanelHeader
        activePanel={activePanel}
        canWriteTable={canWriteTable}
        hasWritableFields={hasWritableFields}
        selectedRecordID={selectedRecordID}
        onOpenHistory={onOpenHistory}
        onOpenRecord={onOpenRecord}
        onOpenView={onOpenView}
      />
      {activePanel === "fields" && (
        <FieldsPanel
          activeFields={activeFields}
          canWriteTable={canWriteTable}
          newFieldName={newFieldName}
          newFieldRequired={newFieldRequired}
          newFieldType={newFieldType}
          onAddField={onAddField}
          onNewFieldNameChange={onNewFieldNameChange}
          onNewFieldRequiredChange={onNewFieldRequiredChange}
          onNewFieldTypeChange={onNewFieldTypeChange}
          onSelectField={onSelectField}
          selectedField={selectedField}
        />
      )}
      {activePanel === "record" && (
        <RecordPanel
          displayedRecordIDs={displayedRecordIDs}
          fields={activeFields}
          onChange={onSelectedRowValueChange}
          onSave={onSaveRecord}
          onSelectRecordID={onSelectRecordID}
          selectedRecordID={selectedRecordID}
          values={selectedRowDraft}
        />
      )}
      {activePanel === "view" && (
        <ViewPanel
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
          onSaveView={onSaveView}
          onSelectTableView={onSelectTableView}
          selectedView={selectedView}
          views={views}
        />
      )}
      {activePanel === "history" && (
        <HistoryPanel
          displayedRecordIDs={displayedRecordIDs}
          onLoadHistory={onLoadHistory}
          onSelectRecordID={onSelectRecordID}
          rowHistory={rowHistory}
          selectedRecordID={selectedRecordID}
        />
      )}
    </div>
  );
}

function CanvasPanelHeader({
  activePanel,
  canWriteTable,
  hasWritableFields,
  selectedRecordID,
  onOpenHistory,
  onOpenRecord,
  onOpenView
}: {
  activePanel: CanvasPanel;
  canWriteTable: boolean;
  hasWritableFields: boolean;
  selectedRecordID: number;
  onOpenHistory: () => void;
  onOpenRecord: () => void;
  onOpenView: () => void;
}) {
  return (
    <Toolbar aria-label="Canvas panel tabs" className="canvas-panel-tabs">
      <ToolbarButton
        aria-label="Record panel"
        appearance={activePanel === "record" ? "primary" : "subtle"}
        onClick={onOpenRecord}
        disabled={!selectedRecordID || !hasWritableFields}
      >
        Record
      </ToolbarButton>
      <ToolbarButton
        aria-label="View panel"
        appearance={activePanel === "view" ? "primary" : "subtle"}
        onClick={onOpenView}
        disabled={!canWriteTable}
      >
        View
      </ToolbarButton>
      <ToolbarButton
        aria-label="History panel"
        appearance={activePanel === "history" ? "primary" : "subtle"}
        onClick={onOpenHistory}
        disabled={!selectedRecordID}
      >
        History
      </ToolbarButton>
    </Toolbar>
  );
}

function FieldsPanel({
  activeFields,
  canWriteTable,
  newFieldName,
  newFieldRequired,
  newFieldType,
  onAddField,
  onNewFieldNameChange,
  onNewFieldRequiredChange,
  onNewFieldTypeChange,
  onSelectField,
  selectedField
}: {
  activeFields: Field[];
  canWriteTable: boolean;
  newFieldName: string;
  newFieldRequired: boolean;
  newFieldType: string;
  onAddField: () => void;
  onNewFieldNameChange: (value: string) => void;
  onNewFieldRequiredChange: (value: boolean) => void;
  onNewFieldTypeChange: (value: string) => void;
  onSelectField: (fieldName: string) => void;
  selectedField?: Field;
}) {
  return (
    <div className="canvas-panel-grid fields-canvas">
      <List
        aria-label="Table fields"
        className="canvas-list"
        navigationMode="items"
        selectedItems={selectedField ? [selectedField.name] : []}
        selectionMode="single"
      >
        {activeFields.map((field) => (
          <ListItem
            key={field.name}
            className="canvas-list-item"
            onAction={() => onSelectField(field.name)}
            value={field.name}
          >
            <span>{field.name}</span>
            <small>
              {field.type}
              {field.required ? " · required" : ""}
            </small>
          </ListItem>
        ))}
      </List>
      <div className="canvas-detail">
        <div className="canvas-detail-header">
          <div>
            <Text weight="semibold">{selectedField?.name ?? "No field selected"}</Text>
            {selectedField && (
              <Text size={200}>
                {selectedField.type}
                {selectedField.required ? " · required" : ""}
              </Text>
            )}
          </div>
        </div>
        <div className="canvas-form-row">
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
          <Button appearance="primary" icon={<AddRegular />} onClick={onAddField} disabled={!canWriteTable}>
            Add Field
          </Button>
        </div>
      </div>
    </div>
  );
}

function ViewPanel({
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
  onSelectTableView,
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
  onSelectTableView: (name: string) => void;
  selectedView?: NonNullable<TableMetadata["views"]>[number];
  views: NonNullable<TableMetadata["views"]>;
}) {
  const canEditView = canWriteTable && Boolean(selectedView);
  return (
    <div className="canvas-panel-grid view-canvas">
      <List
        aria-label="Table views"
        className="canvas-list"
        navigationMode="items"
        selectedItems={[selectedView?.name ?? "all"]}
        selectionMode="single"
      >
        <ListItem className="canvas-list-item" onAction={() => onSelectTableView("all")} value="all">
          <span>All records</span>
          <small>base table</small>
        </ListItem>
        {views.map((viewDef) => (
          <ListItem
            key={viewDef.name}
            className="canvas-list-item"
            onAction={() => onSelectTableView(viewDef.name)}
            value={viewDef.name}
          >
            <span>{viewDef.display_name || viewDef.name}</span>
            <small>{viewDef.base_view ? `based on ${viewDef.base_view}` : "table view"}</small>
          </ListItem>
        ))}
      </List>
      <div className="canvas-detail">
        <div className="canvas-detail-header">
          <div>
            <Text weight="semibold">{selectedView?.display_name || selectedView?.name || "All records"}</Text>
            <Text size={200}>
              {selectedView?.base_view ? `based on ${selectedView.base_view}` : "base table"}
            </Text>
          </div>
        </div>
        <div className="canvas-form-row">
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
          <FluentField label="Filter value">
            <Input
              aria-label="View filter value"
              value={newViewFilterValue}
              onChange={(_, data) => onNewViewFilterValueChange(data.value)}
              disabled={!canEditView || !newViewFilterField || newViewFilterOp === "not_empty"}
            />
          </FluentField>
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
          <Button appearance="primary" icon={<SaveRegular />} onClick={onSaveView} disabled={!canEditView}>
            Save View
          </Button>
        </div>
      </div>
    </div>
  );
}

function RecordPanel({
  displayedRecordIDs,
  fields,
  onChange,
  onSave,
  onSelectRecordID,
  selectedRecordID,
  values
}: {
  displayedRecordIDs: number[];
  fields: Field[];
  onChange: (fieldName: string, value: string) => void;
  onSave: () => void;
  onSelectRecordID: (recordID: number) => void;
  selectedRecordID: number;
  values: Record<string, string>;
}) {
  const writableFields = fields.filter(canWriteField);
  const hasWritableFields = writableFields.length > 0;
  return (
    <div className="canvas-detail record-canvas">
      <div className="canvas-detail-header">
        <div>
          <Text weight="semibold">{selectedRecordID ? `record #${selectedRecordID}` : "No record selected"}</Text>
          <Text size={200}>{hasWritableFields ? "Writable fields" : "No writable fields"}</Text>
        </div>
        <FluentField label="Record">
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
        </FluentField>
      </div>
      <div className="canvas-form-row">
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
    </div>
  );
}

function HistoryPanel({
  displayedRecordIDs,
  onLoadHistory,
  onSelectRecordID,
  rowHistory,
  selectedRecordID
}: {
  displayedRecordIDs: number[];
  onLoadHistory: () => void;
  onSelectRecordID: (recordID: number) => void;
  rowHistory: RowChange[];
  selectedRecordID: number;
}) {
  return (
    <div className="history-canvas" aria-label="Row history">
      <div className="canvas-detail-header">
        <div>
          <Text weight="semibold">Row history</Text>
          <Text size={200}>{selectedRecordID ? `record #${selectedRecordID}` : "No record selected"}</Text>
        </div>
        <div className="history-actions">
          <FluentField label="Record">
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
          </FluentField>
          <Button onClick={onLoadHistory} disabled={!selectedRecordID}>
            Load History
          </Button>
        </div>
      </div>
      <div className="row-history-panel">
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

function canWriteField(field: Field): boolean {
  return (field.permission_level ?? 2) >= 2;
}
