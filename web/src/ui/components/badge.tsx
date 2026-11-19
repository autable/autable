import type { HTMLAttributes } from "react";
import { cn } from "../cn";

/** State semantics: color only on the dot, the text stays neutral so it reads without color vision. */
export type StatusTone = "neutral" | "success" | "running" | "awaiting" | "warning" | "error";

const dotTone: Record<StatusTone, string> = {
  neutral: "bg-subtle",
  success: "bg-success",
  running: "bg-running",
  awaiting: "bg-awaiting",
  warning: "bg-warning",
  error: "bg-error"
};

/** Small label: a gray pill. */
export function Badge({ className, ...props }: HTMLAttributes<HTMLSpanElement>) {
  return (
    <span
      className={cn(
        "inline-flex h-5 items-center gap-1 whitespace-nowrap rounded-full bg-chip px-2 text-12 font-medium text-description [&_svg]:size-3",
        className
      )}
      {...props}
    />
  );
}

/** Status: dot + text. Every page expresses state the same way; ongoing states breathe. */
export function StatusBadge({
  tone,
  label,
  pulse,
  className
}: {
  tone: StatusTone;
  label: string;
  pulse?: boolean;
  className?: string;
}) {
  return (
    <span className={cn("inline-flex items-center gap-1.5 whitespace-nowrap text-12 font-medium text-description", className)}>
      <span className={cn("size-1.5 shrink-0 rounded-full", dotTone[tone], pulse && "animate-breathe")} aria-hidden />
      {label}
    </span>
  );
}

/** Dot-only status for tight lists; the label is its accessible name. */
export function StatusDot({ tone, pulse, label, className }: { tone: StatusTone; pulse?: boolean; label: string; className?: string }) {
  return (
    <span
      role="img"
      aria-label={label}
      title={label}
      className={cn("inline-block size-2 shrink-0 rounded-full", dotTone[tone], pulse && "animate-breathe", className)}
    />
  );
}
