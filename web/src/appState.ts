import type { RoleDefinition, RoleMember } from "./api";

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
