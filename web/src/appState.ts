import type { RoleDefinition, RoleMember } from "./api";

export type WorkspaceViewName = "table" | "workflow" | "app" | "permission";

export type WorkspaceRoute = {
  databaseName: string;
  view: WorkspaceViewName;
  tableName?: string;
  tableViewName?: string;
  workflowID?: number;
  workflowTab?: "editor" | "history";
  workflowRunKey?: string;
  appName?: string;
  roleName?: string;
};

export function replaceResource<T extends { id?: number }>(items: T[], saved: T): T[] {
  if (!saved.id) {
    return items;
  }
  if (!items.some((item) => item.id === saved.id)) {
    return [...items, saved];
  }
  return items.map((item) => (item.id === saved.id ? saved : item));
}

export function replaceRole(items: RoleDefinition[], saved: RoleDefinition): RoleDefinition[] {
  if (!items.some((item) => item.name === saved.name)) {
    return [...items, saved];
  }
  return items.map((item) => (item.name === saved.name ? saved : item));
}

export function compactMembers(members: RoleMember[]): RoleMember[] {
  const byKey = new Map<string, RoleMember>();
  for (const member of members) {
    const type = member.type || "user";
    const id = member.id.trim();
    if (id) {
      byKey.set(`${type}:${id}`, { type, id });
    }
  }
  return [...byKey.values()].sort((left, right) => `${left.type}:${left.id}`.localeCompare(`${right.type}:${right.id}`));
}

export function rowDraftFromRecord(row: Record<string, unknown> | null, fieldNames: string[]): Record<string, string> {
  return Object.fromEntries(fieldNames.map((fieldName) => [fieldName, row?.[fieldName] === undefined ? "" : String(row[fieldName])]));
}

export function parseWorkspaceRoute(pathname: string): WorkspaceRoute | null {
  const segments = pathname.split("/").filter(Boolean).map(decodePathSegment);
  if (segments.length === 0) {
    return null;
  }
  if (segments[0] !== "databases" || !segments[1]) {
    return null;
  }
  const databaseName = segments[1];
  if (segments.length === 2) {
    return { databaseName, view: "table" };
  }
  if (segments[2] === "tables") {
    if (!segments[3]) {
      return { databaseName, view: "table" };
    }
    if (segments[4] === "views" && segments[5]) {
      return { databaseName, view: "table", tableName: segments[3], tableViewName: segments[5] };
    }
    return { databaseName, view: "table", tableName: segments[3] };
  }
  if (segments[2] === "workflows") {
    return {
      databaseName,
      view: "workflow",
      workflowID: parsePositiveInteger(segments[3]),
      workflowTab: segments[4] === "history" ? "history" : "editor",
      workflowRunKey: segments[4] === "history" ? segments[5] : undefined
    };
  }
  if (segments[2] === "apps") {
    return { databaseName, view: "app", appName: segments[3] || undefined };
  }
  if (segments[2] === "permissions") {
    return {
      databaseName,
      view: "permission",
      roleName: segments[3]
    };
  }
  return null;
}

export function buildWorkspacePath(route: WorkspaceRoute | null): string {
  if (!route?.databaseName) {
    return "/";
  }
  const database = encodePathSegment(route.databaseName);
  if (route.view === "workflow") {
    if (!route.workflowID) {
      return `/databases/${database}/workflows`;
    }
    const workflowPath = `/databases/${database}/workflows/${route.workflowID}`;
    if (route.workflowTab !== "history") {
      return workflowPath;
    }
    return route.workflowRunKey
      ? `${workflowPath}/history/${encodePathSegment(route.workflowRunKey)}`
      : `${workflowPath}/history`;
  }
  if (route.view === "app") {
    return route.appName
      ? `/databases/${database}/apps/${encodePathSegment(route.appName)}`
      : `/databases/${database}/apps`;
  }
  if (route.view === "permission") {
    return route.roleName
      ? `/databases/${database}/permissions/${encodePathSegment(route.roleName)}`
      : `/databases/${database}/permissions`;
  }
  if (!route.tableName) {
    return `/databases/${database}`;
  }
  const table = encodePathSegment(route.tableName);
  if (route.tableViewName && route.tableViewName !== "all") {
    return `/databases/${database}/tables/${table}/views/${encodePathSegment(route.tableViewName)}`;
  }
  return `/databases/${database}/tables/${table}`;
}

function parsePositiveInteger(value: string | undefined): number | undefined {
  if (!value || !/^\d+$/.test(value)) {
    return undefined;
  }
  const parsed = Number(value);
  return parsed > 0 ? parsed : undefined;
}

function encodePathSegment(value: string): string {
  return encodeURIComponent(value);
}

function decodePathSegment(value: string): string {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}
