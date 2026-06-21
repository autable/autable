import type { Notify } from "../notifications";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { compactMembers, replaceRole } from "../appState";
import {
  createRole,
  listRoles,
  saveRoleGrants,
  saveRoleMembers,
  searchUsers,
  type AuthUser,
  type DatabaseMetadata,
  type PermissionGrant,
  type RoleDefinition,
  type RoleMember,
  type WorkflowDefinition
} from "../api";
import { compactRoleGrants } from "../permissionState";

type UsePermissionWorkspaceOptions = {
  currentUserID?: string;
  database: DatabaseMetadata;
  onStatus: Notify;
};

export function usePermissionWorkspace({ currentUserID, database, onStatus }: UsePermissionWorkspaceOptions) {
  const { t } = useTranslation();
  const [roles, setRoles] = useState<RoleDefinition[]>([]);
  const [selectedRoleName, setSelectedRoleName] = useState("");
  const [newRoleName, setNewRoleName] = useState("");
  const [roleDraftGrants, setRoleDraftGrants] = useState<PermissionGrant[]>([]);
  const [roleDraftMembers, setRoleDraftMembers] = useState<RoleMember[]>([]);
  const [roleDraftMemberUsers, setRoleDraftMemberUsers] = useState<AuthUser[]>([]);
  const [roleDraftMemberWorkflows, setRoleDraftMemberWorkflows] = useState<WorkflowDefinition[]>([]);
  const [newRoleMemberEmail, setNewRoleMemberEmail] = useState("");
  const [memberSearchResults, setMemberSearchResults] = useState<AuthUser[]>([]);

  const selectedRole = useMemo(
    () => roles.find((item) => item.name === selectedRoleName) ?? roles[0],
    [roles, selectedRoleName]
  );

  useEffect(() => {
    setRoleDraftGrants(selectedRole?.grants ?? []);
    setRoleDraftMembers(selectedRole?.members ?? []);
    setRoleDraftMemberUsers(selectedRole?.member_users ?? []);
    setRoleDraftMemberWorkflows(selectedRole?.member_workflows ?? []);
    setNewRoleMemberEmail("");
    setMemberSearchResults([]);
  }, [selectedRole?.subject_id]);

  useEffect(() => {
    let cancelled = false;
    const query = newRoleMemberEmail.trim();
    if (!currentUserID || query.length < 2) {
      setMemberSearchResults([]);
      return () => {
        cancelled = true;
      };
    }
    void searchUsers(query)
      .then((users) => {
        if (!cancelled) {
          setMemberSearchResults(users);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setMemberSearchResults([]);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [currentUserID, newRoleMemberEmail]);

  useEffect(() => {
    let cancelled = false;
    if (!database.name || !currentUserID) {
      clearRoles();
      return () => {
        cancelled = true;
      };
    }
    void loadRoles(database.name).catch(() => {
      if (!cancelled) {
        clearRoles();
      }
    });
    return () => {
      cancelled = true;
    };

    async function loadRoles(dbName: string) {
      const nextRoles = await listRoles(dbName);
      if (cancelled) {
        return;
      }
      applyRoles(nextRoles);
    }
  }, [currentUserID, database.name]);

  function applyRoles(nextRoles: RoleDefinition[]) {
    setRoles(nextRoles);
    setSelectedRoleName(nextRoles[0]?.name ?? "");
  }

  function clearRoles() {
    applyRoles([]);
  }

  async function refreshRoles(dbName = database.name) {
    if (!currentUserID || !dbName) {
      clearRoles();
      return [];
    }
    const nextRoles = await listRoles(dbName).catch(() => []);
    applyRoles(nextRoles);
    return nextRoles;
  }

  async function createRoleFromSidebar() {
    if (!database.name) {
      onStatus(t("status.selectDatabaseBeforeRole"));
      return;
    }
    const name = newRoleName.trim();
    if (!name) {
      onStatus(t("status.roleNameRequired"));
      return;
    }
    try {
      const saved = await createRole(database.name, name);
      setRoles((current) => replaceRole(current, saved));
      setSelectedRoleName(saved.name);
      setNewRoleName("");
      onStatus(t("status.createdRole", { name: saved.name }));
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.roleCreationFailed"), "error");
    }
  }

  async function persistRoleGrants() {
    if (!database.name || !selectedRole) {
      onStatus(t("status.selectRoleBeforePermissions"));
      return;
    }
    try {
      await saveRoleGrants(database.name, selectedRole.name, compactRoleGrants(roleDraftGrants));
      const saved = await saveRoleMembers(database.name, selectedRole.name, compactMembers(roleDraftMembers));
      setRoles((current) => replaceRole(current, saved));
      setSelectedRoleName(saved.name);
      setRoleDraftMembers(saved.members ?? []);
      setRoleDraftMemberUsers(saved.member_users ?? []);
      setRoleDraftMemberWorkflows(saved.member_workflows ?? []);
      onStatus(t("status.savedRole", { name: saved.name }));
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.roleSaveFailed"), "error");
    }
  }

  function updateRoleGrant(scope: PermissionGrant["scope"], resource: string, field: string, level: PermissionGrant["level"]) {
    if (!selectedRole) {
      return;
    }
    setRoleDraftGrants((current) => {
      const next = current.filter((grant) => grant.scope !== scope || grant.resource !== resource || grant.field !== field);
      if (level === 0) {
        return next;
      }
      return [
        ...next,
        {
          subject_id: selectedRole.subject_id,
          scope,
          resource,
          field,
          level
        }
      ];
    });
  }

  function addRoleMember(user?: AuthUser) {
    const member =
      user ?? memberSearchResults.find((item) => item.email.toLowerCase() === newRoleMemberEmail.trim().toLowerCase());
    if (!member) {
      onStatus(t("status.selectUserSuggestion"));
      return;
    }
    setRoleDraftMembers((current) => compactMembers([...current, { type: "user", id: member.id }]));
    setRoleDraftMemberUsers((current) => compactMemberUsers([...current, member]));
    setNewRoleMemberEmail("");
    setMemberSearchResults([]);
  }

  function addWorkflowMember(workflow: WorkflowDefinition) {
    if (!workflow.id) {
      return;
    }
    setRoleDraftMembers((current) => compactMembers([...current, { type: "workflow", id: String(workflow.id) }]));
    setRoleDraftMemberWorkflows((current) => compactMemberWorkflows([...current, workflow]));
  }

  function removeRoleMember(member: RoleMember) {
    setRoleDraftMembers((current) => current.filter((item) => item.type !== member.type || item.id !== member.id));
    if (member.type === "user") {
      setRoleDraftMemberUsers((current) => current.filter((item) => item.id !== member.id));
      return;
    }
    setRoleDraftMemberWorkflows((current) => current.filter((item) => String(item.id) !== member.id));
  }

  return {
    memberSearchResults,
    newRoleMemberEmail,
    newRoleName,
    roleDraftGrants,
    roleDraftMemberUsers,
    roleDraftMemberWorkflows,
    roleDraftMembers,
    roles,
    selectedRole,
    addRoleMember,
    addWorkflowMember,
    clearRoles,
    createRoleFromSidebar,
    persistRoleGrants,
    refreshRoles,
    removeRoleMember,
    setNewRoleMemberEmail,
    setNewRoleName,
    setSelectedRoleName,
    updateRoleGrant
  };
}

function compactMemberUsers(users: AuthUser[]): AuthUser[] {
  const byID = new Map<string, AuthUser>();
  for (const user of users) {
    if (user.id) {
      byID.set(user.id, user);
    }
  }
  return [...byID.values()].sort((left, right) => left.email.localeCompare(right.email));
}

function compactMemberWorkflows(workflows: WorkflowDefinition[]): WorkflowDefinition[] {
  const byID = new Map<number, WorkflowDefinition>();
  for (const workflow of workflows) {
    if (workflow.id) {
      byID.set(workflow.id, workflow);
    }
  }
  return [...byID.values()].sort((left, right) => left.name.localeCompare(right.name));
}
