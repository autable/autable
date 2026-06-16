import { Button, Select, Text } from "@fluentui/react-components";
import { AddRegular } from "@fluentui/react-icons";
import DataEditor, {
  type EditableGridCell,
  type GridCell,
  type GridColumn,
  type Item
} from "@glideapps/glide-data-grid";
import type { RowChange, TableMetadata } from "../api";

type TableWorkspaceProps = {
  columns: GridColumn[];
  displayedRecordIDs: number[];
  displayedRows: Array<Record<string, unknown>>;
  getCellContent: (cell: Item) => GridCell;
  onAddRow: () => void;
  onCellEdited: (cell: Item, newValue: EditableGridCell) => void | Promise<void>;
  onLoadHistory: () => void;
  onSelectRecordID: (recordID: number) => void;
  rowHistory: RowChange[];
  rows: Array<Record<string, unknown>>;
  selectedRecordID: number;
  table: TableMetadata;
};

export function TableWorkspace({
  columns,
  displayedRecordIDs,
  displayedRows,
  getCellContent,
  onAddRow,
  onCellEdited,
  onLoadHistory,
  onSelectRecordID,
  rowHistory,
  rows,
  selectedRecordID,
  table
}: TableWorkspaceProps) {
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
          <Button icon={<AddRegular />} appearance="primary" onClick={onAddRow}>
            Row
          </Button>
        </div>
      </div>
      <div className="grid-host">
        <DataEditor
          getCellContent={getCellContent}
          onCellEdited={onCellEdited}
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
                <Text size={200}>{new Date(change.timestamp).toLocaleString()}</Text>
              </div>
              <pre>{JSON.stringify(change.values, null, 2)}</pre>
            </div>
          ))
        )}
      </div>
    </div>
  );
}
