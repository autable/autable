import type { ReactNode } from "react";
import { cn } from "../ui";

/** Page frame: title, description and actions on the right; one content width. */
export function Page({
  title,
  description,
  actions,
  icon,
  back,
  children,
  width = "default",
  className
}: {
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  /** A 40px icon tile left of the title, for detail pages. */
  icon?: ReactNode;
  /** A back link above the title, for detail pages. */
  back?: ReactNode;
  children: ReactNode;
  width?: "default" | "wide" | "narrow";
  className?: string;
}) {
  const max = { default: "max-w-5xl", wide: "max-w-6xl", narrow: "max-w-xl" }[width];
  return (
    <div className={cn("mx-auto w-full px-5 py-6 md:px-10 md:py-10", max, className)}>
      {back && <div className="mb-3">{back}</div>}
      <div className="mb-6 flex flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-3">
          {icon && (
            <span className="flex size-10 shrink-0 items-center justify-center rounded-inner bg-chip text-description [&_svg]:size-5">
              {icon}
            </span>
          )}
          <div className="min-w-0">
            <h1 className="truncate text-20 font-medium tracking-tight text-fg">{title}</h1>
            {description && <p className="mt-0.5 text-13 text-muted">{description}</p>}
          </div>
        </div>
        {actions && <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div>}
      </div>
      {children}
    </div>
  );
}

/** A section heading inside a page: a small muted title with an optional action. */
export function SectionHeading({ title, action, id }: { title: ReactNode; action?: ReactNode; id?: string }) {
  return (
    <div className="mb-2 flex min-h-7 items-center justify-between gap-3">
      <h2 id={id} className="text-13 font-medium text-muted">
        {title}
      </h2>
      {action}
    </div>
  );
}
