import { useMemo, useState } from "react";
import {
  Button,
  Input,
  Label,
  Select,
  Tab,
  TabList,
  Text,
  Textarea,
  Toolbar,
  ToolbarButton,
  Tooltip
} from "@fluentui/react-components";
import {
  AddRegular,
  ArrowClockwiseRegular,
  DatabaseRegular,
  PlayRegular,
  SaveRegular
} from "@fluentui/react-icons";
import DataEditor, {
  type GridCell,
  GridCellKind,
  type GridColumn,
  type Item
} from "@glideapps/glide-data-grid";
import { demoCatalog, defaultFormScript, defaultWorkflowScript, initialRows } from "./demoData";
import { previewFormElements } from "./formRuntime";
import { createRow, loadMetadata, saveForm, saveWorkflow, type Catalog } from "./api";

type View = "table" | "workflow" | "form";

export function App() {
  const [catalog, setCatalog] = useState<Catalog>(demoCatalog);
  const [rows, setRows] = useState(initialRows);
  const [view, setView] = useState<View>("table");
  const [selectedTable, setSelectedTable] = useState("contacts");
  const [workflowScript, setWorkflowScript] = useState(defaultWorkflowScript);
  const [formScript, setFormScript] = useState(defaultFormScript);
  const [status, setStatus] = useState("Ready");

  const database = catalog.databases[0];
  const table = database.tables.find((item) => item.name === selectedTable) ?? database.tables[0];
  const activeFields = table.fields.filter((field) => !field.deleted);

  const columns = useMemo<GridColumn[]>(
    () => [
      { id: "record_id", title: "record_id", width: 96 },
      ...activeFields.map((field) => ({
        id: field.name,
        title: field.required ? `${field.name} *` : field.name,
        width: Math.max(128, field.name.length * 14)
      }))
    ],
    [activeFields]
  );

  const getCellContent = ([columnIndex, rowIndex]: Item): GridCell => {
    const column = columns[columnIndex];
    const row = rows[rowIndex];
    const value = row?.[String(column.id)] ?? "";
    return {
      kind: GridCellKind.Text,
      allowOverlay: true,
      displayData: String(value),
      data: String(value)
    };
  };

  async function refreshMetadata() {
    try {
      setCatalog(await loadMetadata());
      setStatus("Metadata refreshed");
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Metadata refresh failed");
    }
  }

  async function addDraftRow() {
    const values = Object.fromEntries(activeFields.map((field) => [field.name, field.name === "status" ? "Review" : ""]));
    values.name = `New record ${rows.length + 1}`;
    try {
      const saved = await createRow(database.name, table.name, values);
      setRows((current) => [...current, { record_id: saved.record_id, ...saved.values }]);
      setStatus(`Created record ${saved.record_id}`);
    } catch (error) {
      const localID = Math.max(0, ...rows.map((row) => Number(row.record_id))) + 1;
      setRows((current) => [...current, { record_id: localID, ...values }]);
      setStatus(error instanceof Error ? `Local draft: ${error.message}` : "Local draft added");
    }
  }

  async function persistWorkflow() {
    try {
      const saved = await saveWorkflow({
        name: "record-review",
        script: workflowScript,
        secrets: { TOKEN: "" },
        variables: { CHANNEL: "ops" }
      });
      setStatus(`Workflow saved as #${saved.id}`);
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Workflow save failed");
    }
  }

  async function persistForm() {
    try {
      const saved = await saveForm({ name: "contact-intake", script: formScript });
      setStatus(`Form saved as #${saved.id}`);
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Form save failed");
    }
  }

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <DatabaseRegular />
          <Text weight="semibold">codetable</Text>
        </div>
        <Label htmlFor="table-select">Table</Label>
        <Select id="table-select" value={selectedTable} onChange={(_, data) => setSelectedTable(data.value)}>
          {database.tables.map((item) => (
            <option key={item.name} value={item.name}>
              {item.display_name || item.name}
            </option>
          ))}
        </Select>
        <div className="metadata-block">
          <Text size={200}>{database.name}</Text>
          <Text size={200}>{database.sqlite_path}</Text>
        </div>
      </aside>

      <main className="workspace">
        <header className="topbar">
          <TabList selectedValue={view} onTabSelect={(_, data) => setView(data.value as View)}>
            <Tab value="table">Table</Tab>
            <Tab value="workflow">Workflow</Tab>
            <Tab value="form">Form</Tab>
          </TabList>
          <Toolbar aria-label="Workspace actions">
            <Tooltip content="Refresh metadata" relationship="label">
              <ToolbarButton aria-label="Refresh metadata" icon={<ArrowClockwiseRegular />} onClick={refreshMetadata} />
            </Tooltip>
            <Tooltip content="Create row" relationship="label">
              <ToolbarButton aria-label="Create row" icon={<AddRegular />} onClick={addDraftRow} />
            </Tooltip>
          </Toolbar>
        </header>

        <section className="content-band">
          {view === "table" && (
            <div className="table-view">
              <div className="section-header">
                <div>
                  <Text weight="semibold">{table.display_name || table.name}</Text>
                  <Text size={200}>{rows.length} records</Text>
                </div>
                <Button icon={<AddRegular />} appearance="primary" onClick={addDraftRow}>
                  Row
                </Button>
              </div>
              <div className="grid-host">
                <DataEditor
                  getCellContent={getCellContent}
                  columns={columns}
                  rows={rows.length}
                  rowMarkers="number"
                  smoothScrollX
                  smoothScrollY
                  width="100%"
                  height="100%"
                />
              </div>
            </div>
          )}

          {view === "workflow" && (
            <div className="split-view">
              <div className="editor-pane">
                <div className="section-header">
                  <div>
                    <Text weight="semibold">record-review.js</Text>
                    <Text size={200}>Default view is JavaScript</Text>
                  </div>
                  <Button icon={<SaveRegular />} appearance="primary" onClick={persistWorkflow}>
                    Save
                  </Button>
                </div>
                <Textarea
                  className="code-editor"
                  value={workflowScript}
                  onChange={(_, data) => setWorkflowScript(data.value)}
                  resize="none"
                  aria-label="Workflow JavaScript"
                />
              </div>
              <div className="history-pane">
                <Text weight="semibold">Run flow</Text>
                <div className="flow-line">
                  <span>trigger.recordChanged</span>
                  <span>table.getRecord</span>
                  <span>notification.send</span>
                </div>
                <Button icon={<PlayRegular />}>Run</Button>
              </div>
            </div>
          )}

          {view === "form" && (
            <div className="split-view">
              <div className="editor-pane">
                <div className="section-header">
                  <div>
                    <Text weight="semibold">contact-intake.js</Text>
                    <Text size={200}>Form script creates elements through the frontend API</Text>
                  </div>
                  <Button icon={<SaveRegular />} appearance="primary" onClick={persistForm}>
                    Save
                  </Button>
                </div>
                <Textarea
                  className="code-editor"
                  value={formScript}
                  onChange={(_, data) => setFormScript(data.value)}
                  resize="none"
                  aria-label="Form JavaScript"
                />
              </div>
              <form className="form-preview">
                {previewFormElements().map((element) => {
                  if (element.kind === "input") {
                    return (
                      <label key={element.name} className="field-stack">
                        <span>{element.label}</span>
                        <Input type={element.inputType} required={element.required} />
                      </label>
                    );
                  }
                  if (element.kind === "select") {
                    return (
                      <label key={element.name} className="field-stack">
                        <span>{element.label}</span>
                        <Select>
                          {element.options.map((option) => (
                            <option key={option}>{option}</option>
                          ))}
                        </Select>
                      </label>
                    );
                  }
                  return (
                    <Button key={element.label} appearance="primary">
                      {element.label}
                    </Button>
                  );
                })}
              </form>
            </div>
          )}
        </section>

        <footer className="statusbar">{status}</footer>
      </main>
    </div>
  );
}
