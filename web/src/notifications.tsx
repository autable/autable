import { useCallback } from "react";
import { useToast } from "./ui";

export type NotifyIntent = "info" | "success" | "error" | "warning";

export type Notify = (message: string, intent?: NotifyIntent) => void;

// Transient toast feedback in place of a persistent status bar. Errors linger
// a little longer (see ToastProvider).
export function useNotifier(): { notify: Notify } {
  const toast = useToast();
  const notify = useCallback<Notify>(
    (message, intent = "info") => {
      toast[intent](message);
    },
    [toast]
  );
  return { notify };
}
