import { useSyncExternalStore } from "react";

/** Matches Tailwind's md breakpoint: 768px and up is the desktop layout (sidebar), below is mobile. */
const query = "(min-width: 768px)";

function subscribe(onChange: () => void) {
  const mql = window.matchMedia?.(query);
  if (!mql) {
    return () => undefined;
  }
  mql.addEventListener("change", onChange);
  return () => mql.removeEventListener("change", onChange);
}

// Environments without matchMedia (tests) get the desktop layout.
function snapshot() {
  return window.matchMedia?.(query).matches ?? true;
}

export function useIsDesktop(): boolean {
  return useSyncExternalStore(subscribe, snapshot, () => true);
}
