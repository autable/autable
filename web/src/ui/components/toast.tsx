import { CheckCircle2, Info, TriangleAlert, X, XCircle } from "lucide-react";
import { Toast as T } from "radix-ui";
import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "../cn";

type Tone = "success" | "error" | "warning" | "info";
interface ToastItem {
  id: number;
  tone: Tone;
  title: string;
  description?: string;
}

const ToastContext = createContext<((t: Omit<ToastItem, "id">) => void) | null>(null);

let nextId = 1;

const icons = { success: CheckCircle2, error: XCircle, warning: TriangleAlert, info: Info };
const tones = { success: "text-success", error: "text-error", warning: "text-warning", info: "text-muted" };

/** Short feedback on the result of an action. Errors also say why, and stay a little longer. */
export function ToastProvider({ children }: { children: ReactNode }) {
  const { t } = useTranslation();
  const [items, setItems] = useState<ToastItem[]>([]);
  const push = useCallback((item: Omit<ToastItem, "id">) => {
    setItems((list) => [...list.slice(-3), { ...item, id: nextId++ }]);
  }, []);
  return (
    <ToastContext.Provider value={push}>
      <T.Provider swipeDirection="right" duration={4000} label={t("ui.notification")}>
        {children}
        {items.map((item) => {
          const Icon = icons[item.tone];
          return (
            <T.Root
              key={item.id}
              duration={item.tone === "error" ? 7000 : undefined}
              onOpenChange={(open) => !open && setItems((list) => list.filter((i) => i.id !== item.id))}
              className="flex items-start gap-3 rounded-container border border-border bg-elevated px-4 py-3 shadow-float animate-rise-in"
            >
              <Icon className={cn("mt-0.5 size-4 shrink-0", tones[item.tone])} aria-hidden />
              <div className="min-w-0 flex-1">
                <T.Title className="text-13 font-medium text-fg [overflow-wrap:anywhere]">{item.title}</T.Title>
                {item.description && <T.Description className="mt-0.5 text-12 text-muted">{item.description}</T.Description>}
              </div>
              <T.Close aria-label={t("common.close")} className="rounded-full text-subtle hover:text-fg">
                <X className="size-4" />
              </T.Close>
            </T.Root>
          );
        })}
        <T.Viewport className="fixed bottom-4 right-4 z-[60] flex w-[calc(100vw-2rem)] max-w-sm flex-col gap-2 outline-none" />
      </T.Provider>
    </ToastContext.Provider>
  );
}

export function useToast() {
  const push = useContext(ToastContext);
  if (!push) throw new Error("useToast must be used inside ToastProvider");
  return useMemo(
    () => ({
      success: (title: string, description?: string) => push({ tone: "success", title, description }),
      error: (title: string, description?: string) => push({ tone: "error", title, description }),
      warning: (title: string, description?: string) => push({ tone: "warning", title, description }),
      info: (title: string, description?: string) => push({ tone: "info", title, description })
    }),
    [push]
  );
}
