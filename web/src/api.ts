export type Field = {
  name: string;
  type: string;
  required: boolean;
  deleted: boolean;
};

export type TableViewFilter = {
  field: string;
  op: "eq" | "contains" | "not_empty";
  value?: unknown;
};

export type TableViewSort = {
  field: string;
  direction: "asc" | "desc";
};

export type TableView = {
  name: string;
  display_name: string;
  base_view?: string;
  filters: TableViewFilter[];
  sorts: TableViewSort[];
};

export type TableMetadata = {
  name: string;
  display_name: string;
  fields: Field[];
  views: TableView[];
};

export type DatabaseMetadata = {
  name: string;
  sqlite_path: string;
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
  timestamp: string;
  values: Record<string, unknown>;
  actor_id?: string;
};

export type WorkflowDefinition = {
  id?: number;
  database_name: string;
  name: string;
  script: string;
  secrets: Record<string, string>;
  variables: Record<string, string>;
};

export type WorkflowPort = {
  name: string;
  type: string;
  description?: string;
  required: boolean;
};

export type WorkflowNodeInfo = {
  type: string;
  display_name: string;
  description?: string;
  inputs: WorkflowPort[];
  outputs: WorkflowPort[];
  stateless: boolean;
  trigger: boolean;
};

export type WorkflowStepRecord = {
  node_id: string;
  input?: Record<string, unknown>;
  output?: Record<string, unknown>;
  error?: string;
};

export type WorkflowRun = {
  workflow_id: number;
  timestamp: string;
  inputs?: Record<string, unknown>;
  outputs?: Record<string, unknown>;
  steps: WorkflowStepRecord[];
  error?: string;
};

export type WorkflowRunResponse = {
  history_key: string;
  run: WorkflowRun;
};

export type FormDefinition = {
  id?: number;
  database_name: string;
  name: string;
  script: string;
};

export type PermissionGrant = {
  subject_id: string;
  scope: "database" | "table" | "field" | "workflow" | "form";
  resource: string;
  field: string;
  level: 0 | 1 | 2;
};

export type RoleDefinition = {
  id?: number;
  database_name: string;
  name: string;
  subject_id: string;
  grants: PermissionGrant[];
};

export type AuthUser = {
  id: string;
  email: string;
  provider: string;
};

export type RowRecord = {
  record_id: number;
  values: Record<string, unknown>;
};

export type OIDCProvider = {
  name: string;
  issuer_url: string;
  scopes: string[];
};

function userHeaders(userID?: string): Record<string, string> {
  return userID ? { "X-Codetable-User": userID } : {};
}

export async function loadMetadata(): Promise<Catalog> {
  const response = await fetch("/api/metadata");
  if (!response.ok) {
    throw new Error(`metadata request failed: ${response.status}`);
  }
  return response.json() as Promise<Catalog>;
}

export async function createDatabase(
  database: Pick<DatabaseMetadata, "name" | "sqlite_path">,
  userID?: string
): Promise<DatabaseMetadata> {
  const response = await fetch("/api/databases", {
    method: "POST",
    headers: { "Content-Type": "application/json", ...userHeaders(userID) },
    body: JSON.stringify(database)
  });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? "database creation failed");
  }
  return response.json() as Promise<DatabaseMetadata>;
}

export async function createTable(dbName: string, table: TableMetadata, userID?: string): Promise<TableMetadata> {
  const response = await fetch(`/api/databases/${dbName}/tables`, {
    method: "POST",
    headers: { "Content-Type": "application/json", ...userHeaders(userID) },
    body: JSON.stringify(table)
  });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? "table creation failed");
  }
  return response.json() as Promise<TableMetadata>;
}

export async function createRow(
  dbName: string,
  tableName: string,
  values: Record<string, unknown>,
  userID?: string
): Promise<RowRecord> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json"
  };
  if (userID) {
    headers["X-Codetable-User"] = userID;
  }
  const response = await fetch(`/api/tables/${dbName}/${tableName}/rows`, {
    method: "POST",
    headers,
    body: JSON.stringify({ values })
  });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? "row creation failed");
  }
  return response.json() as Promise<RowRecord>;
}

export async function updateRow(
  dbName: string,
  tableName: string,
  recordID: number,
  values: Record<string, unknown>,
  userID?: string
): Promise<RowRecord> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json"
  };
  if (userID) {
    headers["X-Codetable-User"] = userID;
  }
  const response = await fetch(`/api/tables/${dbName}/${tableName}/rows/${recordID}`, {
    method: "PATCH",
    headers,
    body: JSON.stringify({ values })
  });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? "row update failed");
  }
  return response.json() as Promise<RowRecord>;
}

export async function listRows(
  dbName: string,
  tableName: string,
  viewName?: string,
  userID?: string
): Promise<RowRecord[]> {
  const query = viewName && viewName !== "all" ? `?view=${encodeURIComponent(viewName)}` : "";
  const response = await fetch(`/api/tables/${dbName}/${tableName}/rows${query}`, {
    headers: userHeaders(userID)
  });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? `row list failed: ${response.status}`);
  }
  return response.json() as Promise<RowRecord[]>;
}

export async function listRowHistory(
  dbName: string,
  tableName: string,
  recordID: number,
  userID?: string
): Promise<RowChange[]> {
  const response = await fetch(`/api/tables/${dbName}/${tableName}/rows/${recordID}/history`, {
    headers: userHeaders(userID)
  });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? `row history failed: ${response.status}`);
  }
  return response.json() as Promise<RowChange[]>;
}

export async function register(email: string, password: string): Promise<AuthUser> {
  const response = await fetch("/api/auth/register", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, password })
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

export async function listOIDCProviders(): Promise<OIDCProvider[]> {
  const response = await fetch("/api/auth/oidc/providers");
  if (!response.ok) {
    throw new Error(`oidc providers failed: ${response.status}`);
  }
  return response.json() as Promise<OIDCProvider[]>;
}

export function oidcStartURL(providerName: string): string {
  return `/api/auth/oidc/${encodeURIComponent(providerName)}/start`;
}

export async function logout(): Promise<void> {
  const response = await fetch("/api/auth/logout", { method: "POST" });
  if (!response.ok) {
    throw new Error(`logout failed: ${response.status}`);
  }
}

export async function listWorkflows(dbName: string, userID?: string): Promise<WorkflowDefinition[]> {
  const response = await fetch(`/api/databases/${dbName}/workflows`, {
    headers: userHeaders(userID)
  });
  if (!response.ok) {
    throw new Error(`workflow list failed: ${response.status}`);
  }
  return response.json() as Promise<WorkflowDefinition[]>;
}

export async function saveWorkflow(
  dbName: string,
  workflow: WorkflowDefinition,
  userID?: string
): Promise<WorkflowDefinition> {
  const response = await fetch(`/api/databases/${dbName}/workflows`, {
    method: "POST",
    headers: { "Content-Type": "application/json", ...userHeaders(userID) },
    body: JSON.stringify(workflow)
  });
  if (!response.ok) {
    throw new Error(`workflow save failed: ${response.status}`);
  }
  return response.json() as Promise<WorkflowDefinition>;
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
  inputs: Record<string, unknown>,
  userID?: string
): Promise<WorkflowRunResponse> {
  const response = await fetch(`/api/workflows/${workflowID}/runs`, {
    method: "POST",
    headers: { "Content-Type": "application/json", ...userHeaders(userID) },
    body: JSON.stringify({ inputs })
  });
  const body = await response.json().catch(() => undefined);
  if (!response.ok && !body?.run) {
    throw new Error(body?.error ?? `workflow run failed: ${response.status}`);
  }
  return body as WorkflowRunResponse;
}

export async function listWorkflowRuns(workflowID: number, userID?: string): Promise<WorkflowRunResponse[]> {
  const response = await fetch(`/api/workflows/${workflowID}/runs`, {
    headers: userHeaders(userID)
  });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? `workflow runs failed: ${response.status}`);
  }
  return response.json() as Promise<WorkflowRunResponse[]>;
}

export async function listForms(dbName: string, userID?: string): Promise<FormDefinition[]> {
  const response = await fetch(`/api/databases/${dbName}/forms`, {
    headers: userHeaders(userID)
  });
  if (!response.ok) {
    throw new Error(`form list failed: ${response.status}`);
  }
  return response.json() as Promise<FormDefinition[]>;
}

export async function saveForm(dbName: string, form: FormDefinition, userID?: string): Promise<FormDefinition> {
  const response = await fetch(`/api/databases/${dbName}/forms`, {
    method: "POST",
    headers: { "Content-Type": "application/json", ...userHeaders(userID) },
    body: JSON.stringify(form)
  });
  if (!response.ok) {
    throw new Error(`form save failed: ${response.status}`);
  }
  return response.json() as Promise<FormDefinition>;
}

export async function listRoles(dbName: string, userID?: string): Promise<RoleDefinition[]> {
  const response = await fetch(`/api/databases/${dbName}/roles`, {
    headers: userHeaders(userID)
  });
  if (!response.ok) {
    throw new Error(`role list failed: ${response.status}`);
  }
  return response.json() as Promise<RoleDefinition[]>;
}

export async function createRole(dbName: string, name: string, userID?: string): Promise<RoleDefinition> {
  const response = await fetch(`/api/databases/${dbName}/roles`, {
    method: "POST",
    headers: { "Content-Type": "application/json", ...userHeaders(userID) },
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
  grants: PermissionGrant[],
  userID?: string
): Promise<RoleDefinition> {
  const response = await fetch(`/api/databases/${dbName}/roles/${encodeURIComponent(roleName)}/grants`, {
    method: "PUT",
    headers: { "Content-Type": "application/json", ...userHeaders(userID) },
    body: JSON.stringify({ grants })
  });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(error.error ?? "role grant save failed");
  }
  return response.json() as Promise<RoleDefinition>;
}
