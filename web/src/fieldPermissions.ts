// Field permission levels are bitmasks mirroring internal/permission:
// read = returned in row values, update = may modify existing rows,
// create = may fill the field when creating a row.
export const FIELD_READ = 1;
export const FIELD_UPDATE = 2;
export const FIELD_CREATE = 4;
export const FIELD_ALL = FIELD_READ | FIELD_UPDATE | FIELD_CREATE;

// An absent annotation means the server did not restrict the field (owner
// paths), so default to full access like the legacy `?? 2` checks did.
export function fieldEditable(level: number | undefined): boolean {
  return ((level ?? FIELD_ALL) & FIELD_UPDATE) !== 0;
}

export function fieldCreatable(level: number | undefined): boolean {
  return ((level ?? FIELD_ALL) & FIELD_CREATE) !== 0;
}
