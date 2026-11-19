import { CircleAlert, type LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "../cn";
import { Button } from "./button";

/** Empty state: one sentence on what will appear here, and the next step. */
export function EmptyState({
  icon: Icon,
  title,
  description,
  action,
  className
}: {
  icon?: LucideIcon;
  title: string;
  description?: ReactNode;
  action?: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("flex flex-col items-center justify-center px-6 py-16 text-center", className)}>
      {Icon && <Icon className="mb-3 size-5 text-subtle" strokeWidth={1.75} aria-hidden />}
      <p className="text-15 font-medium text-fg">{title}</p>
      {description && <div className="mt-1 max-w-sm text-13 text-muted">{description}</div>}
      {action && <div className="mt-5">{action}</div>}
    </div>
  );
}

/** Error state: the reason and the next step, not just an error code. */
export function ErrorState({
  title,
  description,
  onRetry,
  className
}: {
  title?: string;
  description?: ReactNode;
  onRetry?: () => void;
  className?: string;
}) {
  const { t } = useTranslation();
  return (
    <div role="alert" className={cn("flex flex-col items-center justify-center px-6 py-16 text-center", className)}>
      <CircleAlert className="mb-3 size-5 text-error" strokeWidth={1.75} aria-hidden />
      <p className="text-15 font-medium text-fg">{title ?? t("ui.loadFailed")}</p>
      {description && <p className="mt-1 max-w-sm text-13 text-muted">{description}</p>}
      {onRetry && (
        <Button className="mt-5" onClick={onRetry}>
          {t("ui.retry")}
        </Button>
      )}
    </div>
  );
}

/** Inline notice. info is neutral gray; warning and danger color only the icon and title. */
export function Callout({
  tone = "info",
  icon: Icon,
  title,
  children,
  action,
  className
}: {
  tone?: "info" | "warning" | "danger";
  icon?: LucideIcon;
  title?: ReactNode;
  children?: ReactNode;
  action?: ReactNode;
  className?: string;
}) {
  const box = { info: "bg-info-bg border-border-soft", warning: "bg-warning-bg border-transparent", danger: "bg-error-bg border-transparent" }[tone];
  const accent = { info: "text-muted", warning: "text-warning", danger: "text-error" }[tone];
  return (
    <div role={tone === "danger" ? "alert" : "status"} className={cn("flex items-start gap-2.5 rounded-container border px-4 py-3 text-13", box, className)}>
      {Icon && <Icon className={cn("mt-0.5 size-4 shrink-0", accent)} aria-hidden />}
      <div className="min-w-0 flex-1">
        {title && <div className={cn("font-medium", tone === "info" ? "text-fg" : accent)}>{title}</div>}
        {children && <div className={cn("text-description", title && "mt-0.5")}>{children}</div>}
      </div>
      {action && <div className="shrink-0 self-center">{action}</div>}
    </div>
  );
}
