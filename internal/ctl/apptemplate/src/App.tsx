import { useEffect, useState } from "react";
import { Link, Route, Routes } from "react-router";
import { currentUser, table, TABLE_NAMES, type Row, type TableName, type User } from "./autable/autable";

export default function App() {
  const [user, setUser] = useState<User | null>(null);
  useEffect(() => {
    currentUser().then(setUser, () => setUser(null));
  }, []);

  return (
    <main>
      <header>
        <h1>{document.title}</h1>
        {user && <span className="muted">{user.display_name || user.email}</span>}
      </header>
      <nav>
        <Link to="/">Rows</Link> · <Link to="/about">About</Link>
      </nav>
      <Routes>
        <Route path="/" element={<RowsPage />} />
        <Route path="/about" element={<p>Edit src/App.tsx, then run autablectl app push.</p>} />
        <Route path="*" element={<p>Page not found.</p>} />
      </Routes>
    </main>
  );
}

// An example page: the first rows of the database's first table.
function RowsPage() {
  const name: TableName | undefined = TABLE_NAMES.at(0);
  const [rows, setRows] = useState<Row<TableName>[] | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!name) {
      return;
    }
    table(name)
      .list({ sorts: [{ field: "ct_record_id", direction: "desc" }], limit: 20 })
      .then(setRows, (failure: Error) => setError(failure.message));
  }, [name]);

  if (!name) {
    return <p>The database has no tables yet.</p>;
  }
  if (error) {
    return <p className="error">{error}</p>;
  }
  if (!rows) {
    return <p className="muted">Loading…</p>;
  }
  const columns = Array.from(new Set(rows.flatMap((row) => Object.keys(row.values))));
  return (
    <section>
      <h2>{name}</h2>
      <table>
        <thead>
          <tr>
            <th>#</th>
            {columns.map((column) => (
              <th key={column}>{column}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.record_id}>
              <td>{row.record_id}</td>
              {columns.map((column) => (
                <td key={column}>{String((row.values as Record<string, unknown>)[column] ?? "")}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}
