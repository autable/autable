export type Field = {
  name: string;
  type: string;
  value_type?: string;
  formula?: string;
  // For string fields: restricts values to this list (enum). Empty stays
  // allowed; absent means free text.
  options?: string[];
  // Lets an enum hold several options at once, comma-separated.
  multiple?: boolean;
  relation_table?: string;
  deleted: boolean;
  // Bitmask: 1 = read, 2 = update existing rows, 4 = fill on create.
  permission_level?: number;
};

export type TableViewQuery = {
  combinator: "and" | "or";
  rules: TableViewQueryRule[];
  not?: boolean;
};

export type TableViewQueryRule = {
  field?: string;
  operator?: string;
  value?: unknown;
  combinator?: "and" | "or";
  rules?: TableViewQueryRule[];
  not?: boolean;
};

export type TableViewSort = {
  field: string;
  direction: "asc" | "desc";
};

export type TableView = {
  name: string;
  display_name: string;
  base_view?: string;
  query?: TableViewQuery;
  sorts: TableViewSort[];
  permission_level?: 0 | 1 | 2;
};

export type TableMetadata = {
  name: string;
  display_name: string;
  fields: Field[];
  views: TableView[];
  // 2 for admins, 1 for a table a non-admin can see.
  permission_level?: 0 | 1 | 2;
};

export type DatabaseMetadata = {
  name: string;
  tables: TableMetadata[];
};

export type Catalog = {
  databases: DatabaseMetadata[];
};

export type RowChange = {
  history_key: string;
  database: string;
  table: string;
  record_id: number;
  timestamp: number;
  operation?: string;
  values: Record<string, unknown>;
  diff?: Record<string, { old: unknown; new: unknown }>;
  actor_id?: string;
};

export type WorkflowDefinition = {
  id?: number;
  database_name: string;
  name: string;
  script: string;
  enabled?: boolean;
  creator_id?: string;
  secrets: Record<string, number>;
  secret_values?: Record<string, string>;
  variables: Record<string, string>;
  runners?: Record<string, string>;
  history_retention_days?: number | null;
  timeout_seconds?: number | null;
  permission_level?: 0 | 1 | 2;
  created_at?: number;
  updated_at?: number;
};

export type WorkflowPort = {
  name: string;
  type: string;
  description?: string;
};

export type WorkflowNodeInfo = {
  type: string;
  display_name: string;
  description?: string;
  documentation?: Record<string, string>;
  inputs: WorkflowPort[];
  outputs: WorkflowPort[];
  variables?: WorkflowPort[];
  secrets?: WorkflowPort[];
  stateless: boolean;
  trigger: boolean;
};

export type WorkflowInstanceDeclaration = {
  node: string;
  variables?: WorkflowPort[];
  secrets?: WorkflowPort[];
  params?: Record<string, unknown>;
};

export type WorkflowStepRecord = {
  node_id: string;
  node_type?: string;
  runner?: string;
  input?: Record<string, unknown>;
  output?: Record<string, unknown>;
  error?: string;
};

export type RunnerStatus = {
  name: string;
  version: string;
  node_types: string[];
  connected_at: number;
};

export type RunnersResponse = {
  token: { exists: boolean; created_at?: number };
  runners: RunnerStatus[];
  remote_node_types: string[];
};

export type WorkflowRun = {
  workflow_id: number;
  timestamp: number;
  inputs?: Record<string, unknown>;
  outputs?: Record<string, unknown>;
  steps: WorkflowStepRecord[];
  error?: string;
};

export type WorkflowRunResponse = {
  history_key: string;
  run: WorkflowRun;
  summary?: boolean;
};

// AppDefinition is a hosted web app: a versioned static bundle uploaded with
// `autablectl app push` and served at `url`. published_version is 0 while
// no version is published.
export type AppDefinition = {
  id: number;
  database_name: string;
  name: string;
  title: string;
  published_version: number;
  latest_version: number;
  creator_id?: string;
  permission_level?: 0 | 1 | 2;
  created_at: number;
  updated_at: number;
  url: string;
};

export type AppVersion = {
  app_id: number;
  version: number;
  sdk: string;
  message: string;
  size: number;
  created_by: string;
  created_at: number;
  supported: boolean;
  sdk_error?: string;
  preview_url: string;
};

export type AppDetail = {
  app: AppDefinition;
  versions: AppVersion[];
  sdk: { version: string; min_major: number };
};

export type PermissionGrant = {
  subject_id: string;
  scope:
    | "field_set"
    | "field"
    | "record"
    | "view_set"
    | "view"
    | "workflow"
    | "app_set"
    | "app"
    | "file";
  resource: string;
  field: string;
  // 0/1/2 for most data scopes; field and field_set use the bitmask
  // 1 = read, 2 = update, 4 = create. workflow is 0 = none / 1 = run;
  // app and app_set are 0 = none / 1 = open.
  level: number;
};

export type RoleDefinition = {
  id?: number;
  database_name: string;
  name: string;
  subject_id: string;
  grants: PermissionGrant[];
  members: RoleMember[];
  member_users?: AuthUser[];
  member_workflows?: WorkflowDefinition[];
  created_at?: number;
  updated_at?: number;
};

export type RoleMember = {
  type: "user" | "workflow";
  id: string;
};

export type AuthUser = {
  id: string;
  email: string;
  display_name: string;
  provider: string;
  // Global admins write code and structure; everyone else only gets data
  // access through grants.
  is_admin?: boolean;
};

export type RowRecord = {
  record_id: number;
  values: Record<string, unknown>;
};

export type RowListOptions = {
  view?: string;
  query?: TableViewQuery | { field: string; op?: string; operator?: string; value?: unknown };
  sorts?: TableViewSort[];
  limit?: number;
  offset?: number;
  search?: string;
};

export type RowPage = {
  rows: RowRecord[];
  total: number;
};

export type RowMutation = RowRecord & {
  operation: "create" | "update" | "noop";
};

export type FieldMutation = {
  created: Field[];
  restored: Field[];
  existing: Field[];
  fields: Field[];
};

export type OIDCProvider = {
  name: string;
  issuer_url: string;
  scopes: string[];
};

export type AuthConfig = {
  password_enabled: boolean;
  oidc_enabled: boolean;
  oidc_providers: OIDCProvider[];
};

export async function loadMetadata(): Promise<Catalog> {
  const response = await fetch("/api/metadata");
  if (!response.ok) {
    throw new Error(`metadata request failed: ${response.status}`);
  }
  return response.json() as Promise<Catalog>;
}

export async function createDatabase(database: Pick<DatabaseMetadata, "name">): Promise<DatabaseMetadata> {
  const response = await fetch("/api/databases", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(database)
  });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? "database creation failed");
  }
  return response.json() as Promise<DatabaseMetadata>;
}

export async function createTable(dbName: string, table: TableMetadata): Promise<TableMetadata> {
  const response = await fetch(`/api/databases/${dbName}/tables`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(table)
  });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? "table creation failed");
  }
  return response.json() as Promise<TableMetadata>;
}

export async function updateTableMetadata(
  dbName: string,
  tableName: string,
  table: TableMetadata
): Promise<TableMetadata> {
  const response = await fetch(`/api/databases/${dbName}/tables/${tableName}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(table)
  });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? "table metadata update failed");
  }
  return response.json() as Promise<TableMetadata>;
}

export type FieldPositionRequest =
  | { position: "start"; before?: never; after?: never }
  | { before: string; position?: never; after?: never }
  | { after: string; position?: never; before?: never };

export async function moveTableFieldPosition(
  dbName: string,
  tableName: string,
  fieldName: string,
  position: FieldPositionRequest
): Promise<TableMetadata> {
  const response = await fetch(
    `/api/databases/${encodeURIComponent(dbName)}/tables/${encodeURIComponent(tableName)}/fields/${encodeURIComponent(fieldName)}/position`,
    {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(position)
    }
  );
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? "field position update failed");
  }
  return response.json() as Promise<TableMetadata>;
}

export async function createRow(
  dbName: string,
  tableName: string,
  values: Record<string, unknown>
): Promise<RowRecord> {
  const response = await fetch(`/api/tables/${dbName}/${tableName}/rows`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ values })
  });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? "row creation failed");
  }
  return response.json() as Promise<RowRecord>;
}

export async function upsertRow(
  dbName: string,
  tableName: string,
  matchField: string,
  values: Record<string, unknown>
): Promise<RowMutation> {
  const response = await fetch(`/api/tables/${dbName}/${tableName}/rows/upsert`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ match_field: matchField, values })
  });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? "row upsert failed");
  }
  return response.json() as Promise<RowMutation>;
}

export async function createFields(
  dbName: string,
  tableName: string,
  fields: Field[] | Record<string, string>
): Promise<FieldMutation> {
  const response = await fetch(`/api/tables/${dbName}/${tableName}/fields`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ fields })
  });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? "field creation failed");
  }
  return response.json() as Promise<FieldMutation>;
}

export async function updateRow(
  dbName: string,
  tableName: string,
  recordID: number,
  values: Record<string, unknown>
): Promise<RowRecord> {
  const response = await fetch(`/api/tables/${dbName}/${tableName}/rows/${recordID}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ values })
  });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? "row update failed");
  }
  return response.json() as Promise<RowRecord>;
}

export async function deleteRow(dbName: string, tableName: string, recordID: number): Promise<RowRecord> {
  const response = await fetch(`/api/tables/${dbName}/${tableName}/rows/${recordID}`, {
    method: "DELETE"
  });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? "row deletion failed");
  }
  return response.json() as Promise<RowRecord>;
}

export async function listRows(
  dbName: string,
  tableName: string,
  viewName?: string,
  sort?: TableViewSort,
  options?: RowListOptions
): Promise<RowRecord[]> {
  if (options?.query || options?.limit || options?.sorts?.length || options?.view) {
    const response = await fetch(`/api/tables/${dbName}/${tableName}/rows/query`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        view: options.view ?? viewName,
        query: normalizeRowListQuery(options.query),
        sorts: options.sorts ?? (sort ? [sort] : undefined),
        limit: options.limit
      })
    });
    if (!response.ok) {
      const error = await response.json().catch(() => ({ error: response.statusText }));
      throw new Error(error.error ?? `row query failed: ${response.status}`);
    }
    return response.json() as Promise<RowRecord[]>;
  }
  const params = new URLSearchParams();
  if (viewName && viewName !== "all") {
    params.set("view", viewName);
  }
  if (sort) {
    params.set("sort_field", sort.field);
    params.set("sort_direction", sort.direction);
  }
  const query = params.toString();
  const response = await fetch(`/api/tables/${dbName}/${tableName}/rows${query ? `?${query}` : ""}`);
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? `row list failed: ${response.status}`);
  }
  return response.json() as Promise<RowRecord[]>;
}

export async function listRowsPage(dbName: string, tableName: string, options: RowListOptions): Promise<RowPage> {
  const response = await fetch(`/api/tables/${dbName}/${tableName}/rows/page`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      view: options.view && options.view !== "all" ? options.view : undefined,
      query: normalizeRowListQuery(options.query),
      sorts: options.sorts?.length ? options.sorts : undefined,
      limit: options.limit,
      offset: options.offset || undefined,
      search: options.search || undefined
    })
  });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? `row page failed: ${response.status}`);
  }
  return response.json() as Promise<RowPage>;
}

function normalizeRowListQuery(query: RowListOptions["query"]): TableViewQuery | undefined {
  if (!query) {
    return undefined;
  }
  if ("rules" in query) {
    return query;
  }
  return {
    combinator: "and",
    rules: [{ field: query.field, operator: query.operator ?? query.op ?? "=", value: query.value }]
  };
}

export async function listRowHistory(
  dbName: string,
  tableName: string,
  recordID: number
): Promise<RowChange[]> {
  const response = await fetch(`/api/tables/${dbName}/${tableName}/rows/${recordID}/history`);
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? `row history failed: ${response.status}`);
  }
  return response.json() as Promise<RowChange[]>;
}

export async function register(email: string, password: string, displayName: string): Promise<AuthUser> {
  const response = await fetch("/api/auth/register", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, password, display_name: displayName })
  });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? "registration failed");
  }
  return response.json() as Promise<AuthUser>;
}

export async function login(email: string, password: string): Promise<AuthUser> {
  const response = await fetch("/api/auth/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, password })
  });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? "login failed");
  }
  return response.json() as Promise<AuthUser>;
}

export async function loadCurrentUser(): Promise<AuthUser | null> {
  const response = await fetch("/api/auth/me");
  if (response.status === 401) {
    return null;
  }
  if (!response.ok) {
    throw new Error(`current user request failed: ${response.status}`);
  }
  return response.json() as Promise<AuthUser>;
}

export async function searchUsers(query: string): Promise<AuthUser[]> {
  const response = await fetch(`/api/users?query=${encodeURIComponent(query)}`);
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? `user search failed: ${response.status}`);
  }
  return response.json() as Promise<AuthUser[]>;
}

export async function loadAuthConfig(): Promise<AuthConfig> {
  const response = await fetch("/api/auth/config");
  if (!response.ok) {
    throw new Error(`auth config failed: ${response.status}`);
  }
  return response.json() as Promise<AuthConfig>;
}

export function oidcStartURL(providerName: string, redirect?: string): string {
  const base = `/api/auth/oidc/${encodeURIComponent(providerName)}/start`;
  return redirect ? `${base}?redirect=${encodeURIComponent(redirect)}` : base;
}

export async function logout(): Promise<void> {
  const response = await fetch("/api/auth/logout", { method: "POST" });
  if (!response.ok) {
    throw new Error(`logout failed: ${response.status}`);
  }
}

export async function fetchRunners(dbName: string): Promise<RunnersResponse> {
  const response = await fetch(`/api/databases/${dbName}/runners`);
  if (!response.ok) {
    throw new Error(`runners load failed: ${response.status}`);
  }
  return response.json() as Promise<RunnersResponse>;
}

export async function resetRunnerToken(dbName: string): Promise<{ token: string; created_at: number }> {
  const response = await fetch(`/api/databases/${dbName}/runners`, { method: "POST" });
  if (!response.ok) {
    throw new Error(`runner token reset failed: ${response.status}`);
  }
  return response.json() as Promise<{ token: string; created_at: number }>;
}

export type FileRecord = {
  id: number;
  name: string;
  size: number;
  content_type: string;
  database_name: string;
  table_name: string;
  record_id: number;
  created_at?: number;
};

export async function uploadFile(file: File, databaseName: string, tableName: string, recordID: number): Promise<FileRecord> {
  const body = new FormData();
  body.append("file", file);
  body.append("database_name", databaseName);
  body.append("table_name", tableName);
  body.append("record_id", String(recordID));
  const response = await fetch("/api/files", { method: "POST", body });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? `file upload failed: ${response.status}`);
  }
  return response.json() as Promise<FileRecord>;
}

export async function fetchFileMetadata(ids: number[]): Promise<FileRecord[]> {
  const response = await fetch("/api/files/metadata", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ ids })
  });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? `file metadata failed: ${response.status}`);
  }
  return response.json() as Promise<FileRecord[]>;
}

export function fileDownloadURL(id: number): string {
  return `/api/files/${id}`;
}

export async function listWorkflows(dbName: string): Promise<WorkflowDefinition[]> {
  const response = await fetch(`/api/databases/${dbName}/workflows`);
  if (!response.ok) {
    throw new Error(`workflow list failed: ${response.status}`);
  }
  return response.json() as Promise<WorkflowDefinition[]>;
}

export async function saveWorkflow(
  dbName: string,
  workflow: WorkflowDefinition
): Promise<WorkflowDefinition> {
  const payload = {
    id: workflow.id,
    database_name: workflow.database_name,
    name: workflow.name,
    script: workflow.script,
    enabled: workflow.enabled ?? true,
    creator_id: workflow.creator_id,
    secrets: workflow.secret_values ?? {},
    variables: workflow.variables,
    runners: workflow.runners ?? {},
    history_retention_days: workflow.history_retention_days ?? null,
    timeout_seconds: workflow.timeout_seconds ?? null,
    permission_level: workflow.permission_level,
    created_at: workflow.created_at,
    updated_at: workflow.updated_at
  };
  const response = await fetch(`/api/databases/${dbName}/workflows`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload)
  });
  if (!response.ok) {
    throw new Error(`workflow save failed: ${response.status}`);
  }
  return response.json() as Promise<WorkflowDefinition>;
}

export async function deleteWorkflow(workflowID: number): Promise<void> {
  const response = await fetch(`/api/workflows/${workflowID}`, { method: "DELETE" });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? `workflow delete failed: ${response.status}`);
  }
}

export async function loadWorkflowNodes(): Promise<WorkflowNodeInfo[]> {
  const response = await fetch("/api/workflow/nodes");
  if (!response.ok) {
    throw new Error(`workflow nodes failed: ${response.status}`);
  }
  return response.json() as Promise<WorkflowNodeInfo[]>;
}

export async function runWorkflow(
  workflowID: number,
  inputs: Record<string, unknown>
): Promise<WorkflowRunResponse> {
  const response = await fetch(`/api/workflows/${workflowID}/runs`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ inputs })
  });
  const body = await response.json().catch(() => undefined);
  if (!response.ok && !body?.run) {
    throw new Error(body?.error ?? `workflow run failed: ${response.status}`);
  }
  return body as WorkflowRunResponse;
}

export async function listWorkflowRuns(workflowID: number): Promise<WorkflowRunResponse[]> {
  const response = await fetch(`/api/workflows/${workflowID}/runs`);
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? `workflow runs failed: ${response.status}`);
  }
  return response.json() as Promise<WorkflowRunResponse[]>;
}

export async function loadWorkflowRun(workflowID: number, historyKey: string): Promise<WorkflowRunResponse> {
  const response = await fetch(`/api/workflows/${workflowID}/runs/${encodeURIComponent(historyKey)}`);
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? `workflow run failed: ${response.status}`);
  }
  return response.json() as Promise<WorkflowRunResponse>;
}

export async function listApps(dbName: string): Promise<AppDefinition[]> {
  const response = await fetch(`/api/databases/${encodeURIComponent(dbName)}/apps`);
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? `app list failed: ${response.status}`);
  }
  return response.json() as Promise<AppDefinition[]>;
}

function appPath(dbName: string, appName: string): string {
  return `/api/databases/${encodeURIComponent(dbName)}/apps/${encodeURIComponent(appName)}`;
}

export async function loadApp(dbName: string, appName: string): Promise<AppDetail> {
  const response = await fetch(appPath(dbName, appName));
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? `app load failed: ${response.status}`);
  }
  return response.json() as Promise<AppDetail>;
}

export async function publishAppVersion(dbName: string, appName: string, version: number): Promise<AppDefinition> {
  const response = await fetch(`${appPath(dbName, appName)}/publish`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ version })
  });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? `app publish failed: ${response.status}`);
  }
  return response.json() as Promise<AppDefinition>;
}

export async function deleteApp(dbName: string, appName: string): Promise<void> {
  const response = await fetch(appPath(dbName, appName), { method: "DELETE" });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? `app delete failed: ${response.status}`);
  }
}

export function appSourceURL(dbName: string, appName: string, version: number): string {
  return `${appPath(dbName, appName)}/versions/${version}/source`;
}

export async function listRoles(dbName: string): Promise<RoleDefinition[]> {
  const response = await fetch(`/api/databases/${dbName}/roles`);
  if (!response.ok) {
    throw new Error(`role list failed: ${response.status}`);
  }
  return response.json() as Promise<RoleDefinition[]>;
}

export async function createRole(dbName: string, name: string): Promise<RoleDefinition> {
  const response = await fetch(`/api/databases/${dbName}/roles`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name })
  });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? "role creation failed");
  }
  return response.json() as Promise<RoleDefinition>;
}

export async function saveRoleGrants(
  dbName: string,
  roleName: string,
  grants: PermissionGrant[]
): Promise<RoleDefinition> {
  const response = await fetch(`/api/databases/${dbName}/roles/${encodeURIComponent(roleName)}/grants`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ grants })
  });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? "role grant save failed");
  }
  return response.json() as Promise<RoleDefinition>;
}

export async function saveRoleMembers(
  dbName: string,
  roleName: string,
  members: RoleMember[]
): Promise<RoleDefinition> {
  const response = await fetch(`/api/databases/${dbName}/roles/${encodeURIComponent(roleName)}/members`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ members })
  });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? "role member save failed");
  }
  return response.json() as Promise<RoleDefinition>;
}
