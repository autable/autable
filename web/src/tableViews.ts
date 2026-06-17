import type { TableView } from "./api";

export function applyTableView<T extends Record<string, unknown>>(rows: T[], views: TableView[], selectedView: string): T[] {
  if (selectedView === "all") {
    return rows;
  }
  const resolved = resolveTableView(views, selectedView, new Set());
  if (!resolved) {
    return rows;
  }
  const filtered = rows.filter((row) =>
    resolved.filters.every((filter) => {
      const value = rowValue(row, filter.field);
      if (filter.op === "eq") {
        return String(value) === String(filter.value);
      }
      if (filter.op === "contains") {
        return String(value).toLowerCase().includes(String(filter.value ?? "").toLowerCase());
      }
      if (filter.op === "not_empty") {
        return value !== undefined && value !== null && String(value).trim() !== "";
      }
      return false;
    })
  );
  return [...filtered].sort((left, right) => {
    for (const sortDef of resolved.sorts) {
      const leftValue = String(rowValue(left, sortDef.field));
      const rightValue = String(rowValue(right, sortDef.field));
      if (leftValue === rightValue) {
        continue;
      }
      return sortDef.direction === "desc" ? rightValue.localeCompare(leftValue) : leftValue.localeCompare(rightValue);
    }
    return Number(left.record_id ?? 0) - Number(right.record_id ?? 0);
  });
}

export function resolveTableView(views: TableView[], name: string, visiting: Set<string>): TableView | undefined {
  const view = views.find((item) => item.name === name);
  if (!view || visiting.has(name)) {
    return undefined;
  }
  visiting.add(name);
  if (!view.base_view) {
    visiting.delete(name);
    return view;
  }
  const base = resolveTableView(views, view.base_view, visiting);
  visiting.delete(name);
  if (!base) {
    return view;
  }
  return {
    ...view,
    filters: [...base.filters, ...view.filters],
    sorts: [...base.sorts, ...view.sorts]
  };
}

function rowValue(row: Record<string, unknown>, field: string) {
  return row[field];
}
