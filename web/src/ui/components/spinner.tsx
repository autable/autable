import { useTranslation } from "react-i18next";
import { cn } from "../cn";

/** Loading: a thin 16px ring in the text color; still under reduced motion. */
export function Spinner({ className, label }: { className?: string; label?: string }) {
  const { t } = useTranslation();
  return (
    <svg viewBox="0 0 16 16" className={cn("size-4 animate-spin text-current", className)} role="status" aria-label={label ?? t("ui.loading")}>
      <circle cx="8" cy="8" r="6.25" fill="none" stroke="currentColor" strokeOpacity="0.2" strokeWidth="1.5" />
      <path d="M14.25 8A6.25 6.25 0 0 0 8 1.75" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  );
}

/** Placeholder skeleton: stands in for content while it loads so the layout does not jump. */
export function Skeleton({ className }: { className?: string }) {
  return <div className={cn("animate-pulse rounded-inner bg-chip/70", className)} aria-hidden />;
}
