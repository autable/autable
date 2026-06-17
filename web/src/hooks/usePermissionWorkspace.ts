import { useEffect, useMemo, useState } from "react";
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
  type RoleDefinition
} from "../api";
import { compactRoleGrants } from "../permissionState";

type UsePermissionWorkspaceOptions = {
  currentUserID?: string;
  database: DatabaseMetadata;
  onStatus: (message: string) => void;
};

export function usePermissionWorkspace({ currentUserID, database, onStatus }: UsePermissionWorkspaceOptions) {
  const [roles, setRoles] = useState<RoleDefinition[]>([]);
  const [selectedRoleName, setSelectedRoleName] = useState("");
  const [newRoleName, setNewRoleName] = useState("");
  const [roleDraftGrants, setRoleDraftGrants] = useState<PermissionGrant[]>([]);
  const [roleDraftMembers, setRoleDraftMembers] = useState<string[]>([]);
  const [roleDraftMemberUsers, setRoleDraftMemberUsers] = useState<AuthUser[]>([]);
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
      onStatus("Select a database before creating a role");
      return;
    }
    const name = newRoleName.trim();
    if (!name) {
      onStatus("Role name is required");
      return;
    }
    try {
      const saved = await createRole(database.name, name);
      setRoles((current) => replaceRole(current, saved));
      setSelectedRoleName(saved.name);
      setNewRoleName("");
      onStatus(`Created role ${saved.name}`);
    } catch (error) {
      onStatus(error instanceof Error ? error.message : "Role creation failed");
    }
  }

  async function persistRoleGrants() {
    if (!database.name || !selectedRole) {
      onStatus("Select a role before saving permissions");
      return;
    }
    try {
      await saveRoleGrants(database.name, selectedRole.name, compactRoleGrants(roleDraftGrants, database));
      const saved = await saveRoleMembers(database.name, selectedRole.name, compactMembers(roleDraftMembers));
      setRoles((current) => replaceRole(current, saved));
      setSelectedRoleName(saved.name);
      setRoleDraftMembers(saved.members ?? []);
      setRoleDraftMemberUsers(saved.member_users ?? []);
      onStatus(`Saved role ${saved.name}`);
    } catch (error) {
      onStatus(error instanceof Error ? error.message : "Role save failed");
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
      onStatus("Select a user email from the member suggestions");
      return;
    }
    setRoleDraftMembers((current) => compactMembers([...current, member.id]));
    setRoleDraftMemberUsers((current) => compactMemberUsers([...current, member]));
    setNewRoleMemberEmail("");
    setMemberSearchResults([]);
  }

  function removeRoleMember(memberID: string) {
    setRoleDraftMembers((current) => current.filter((item) => item !== memberID));
    setRoleDraftMemberUsers((current) => current.filter((item) => item.id !== memberID));
  }

  return {
    memberSearchResults,
    newRoleMemberEmail,
    newRoleName,
    roleDraftGrants,
    roleDraftMemberUsers,
    roleDraftMembers,
    roles,
    selectedRole,
    addRoleMember,
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
