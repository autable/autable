import { cn } from "../ui";

/** Monochrome mark (a table in a rounded tile): follows the text color, so it works light and dark. */
export function LogoMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" className={cn("size-6 shrink-0", className)} aria-hidden>
      <rect width="24" height="24" rx="7" className="fill-fg" />
      <rect x="6" y="6.5" width="12" height="11" rx="2.2" fill="none" className="stroke-surface" strokeWidth="1.8" />
      <path d="M6 10.6h12M10.6 10.6v6.9" fill="none" className="stroke-surface" strokeWidth="1.8" />
    </svg>
  );
}

export function Wordmark({ className }: { className?: string }) {
  return (
    <span className={cn("flex items-center gap-2 text-15 font-medium tracking-tight text-fg", className)}>
      <LogoMark />
      autable
    </span>
  );
}
