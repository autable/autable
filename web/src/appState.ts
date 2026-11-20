import type { RoleDefinition, RoleMember } from "./api";

// home: the database landing page (apps and tables); admin: the management
// overview, admins only.
// library: the instance-wide libraries, admins only; shown inside the
// current database's workspace like the other management pages.
export type WorkspaceViewName = "home" | "table" | "workflow" | "app" | "permission" | "admin" | "library";

// The sections of a workflow's page; "config" holds its secrets and vars.
export type WorkflowTab = "overview" | "runs" | "versions" | "config";

const workflowTabs: WorkflowTab[] = ["overview", "runs", "versions", "config"];

export type WorkspaceRoute = {
  databaseName: string;
  view: WorkspaceViewName;
  tableName?: string;
  tableViewName?: string;
  workflowName?: string;
  workflowTab?: WorkflowTab;
  workflowRunID?: number;
  appName?: string;
  roleName?: string;
  libraryName?: string;
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
    return { databaseName, view: "home" };
  }
  if (segments[2] === "admin" && segments.length === 3) {
    return { databaseName, view: "admin" };
  }
  if (segments[2] === "tables") {
    if (!segments[3]) {
      return { databaseName, view: "home" };
    }
    if (segments[4] === "views" && segments[5]) {
      return { databaseName, view: "table", tableName: segments[3], tableViewName: segments[5] };
    }
    return { databaseName, view: "table", tableName: segments[3] };
  }
  if (segments[2] === "workflows") {
    if (!segments[3]) {
      return { databaseName, view: "workflow" };
    }
    const tab = workflowTabs.find((item) => item === segments[4] && item !== "overview");
    if (segments[4] && !tab) {
      return null;
    }
    return {
      databaseName,
      view: "workflow",
      workflowName: segments[3],
      workflowTab: tab ?? "overview",
      workflowRunID: tab === "runs" ? parsePositiveInteger(segments[5]) : undefined
    };
  }
  if (segments[2] === "apps") {
    return { databaseName, view: "app", appName: segments[3] || undefined };
  }
  if (segments[2] === "libraries" && segments.length <= 4) {
    return { databaseName, view: "library", libraryName: segments[3] || undefined };
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
  if (route.view === "admin") {
    return `/databases/${database}/admin`;
  }
  if (route.view === "workflow") {
    if (!route.workflowName) {
      return `/databases/${database}/workflows`;
    }
    const workflowPath = `/databases/${database}/workflows/${encodePathSegment(route.workflowName)}`;
    if (!route.workflowTab || route.workflowTab === "overview") {
      return workflowPath;
    }
    if (route.workflowTab === "runs" && route.workflowRunID) {
      return `${workflowPath}/runs/${route.workflowRunID}`;
    }
    return `${workflowPath}/${route.workflowTab}`;
  }
  if (route.view === "app") {
    return route.appName
      ? `/databases/${database}/apps/${encodePathSegment(route.appName)}`
      : `/databases/${database}/apps`;
  }
  if (route.view === "library") {
    return route.libraryName
      ? `/databases/${database}/libraries/${encodePathSegment(route.libraryName)}`
      : `/databases/${database}/libraries`;
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
