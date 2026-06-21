import { useCallback, type ReactElement } from "react";
import { Toast, Toaster, ToastTitle, useId, useToastController } from "@fluentui/react-components";

export type NotifyIntent = "info" | "success" | "error" | "warning";

export type Notify = (message: string, intent?: NotifyIntent) => void;

// Transient toast feedback in place of a persistent status bar. Errors linger
// a little longer and stay until dismissed if they need attention.
export function useNotifier(): { toasterId: string; Toaster: () => ReactElement; notify: Notify } {
  const toasterId = useId("workspace-toaster");
  const { dispatchToast } = useToastController(toasterId);

  const notify = useCallback<Notify>(
    (message, intent = "info") => {
      dispatchToast(
        <Toast>
          <ToastTitle>{message}</ToastTitle>
        </Toast>,
        { intent, timeout: intent === "error" ? 7000 : 3000 }
      );
    },
    [dispatchToast]
  );

  const ToasterSlot = useCallback(
    () => <Toaster toasterId={toasterId} position="bottom-end" pauseOnHover />,
    [toasterId]
  );

  return { toasterId, Toaster: ToasterSlot, notify };
}
