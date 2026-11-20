import { cva, type VariantProps } from "class-variance-authority";
import { Slot } from "radix-ui";
import { forwardRef, type ButtonHTMLAttributes } from "react";
import { cn } from "../cn";
import { Spinner } from "./spinner";

/**
 * Buttons are pills, 13px / 500. Hover and press mix each fill toward the
 * text color instead of fading it.
 * - primary: gray pill, everyday main actions
 * - secondary: white pill with a 1px border
 * - cta: dark pill, the most emphasized action, at most one per screen
 * - quiet: borderless, icon buttons and minor actions
 * - danger: confirms a destructive action; danger-quiet: borderless destructive action
 */
const buttonVariants = cva(
  "relative inline-flex shrink-0 select-none items-center justify-center gap-1.5 whitespace-nowrap rounded-full text-13 font-medium transition-colors duration-150 disabled:pointer-events-none disabled:opacity-60 aria-disabled:pointer-events-none aria-disabled:opacity-60 [&_svg]:size-4 [&_svg]:shrink-0",
  {
    variants: {
      variant: {
        primary:
          "bg-chip text-fg hover:bg-[color-mix(in_oklab,var(--chip),var(--text-primary)_8%)] active:bg-[color-mix(in_oklab,var(--chip),var(--text-primary)_17%)]",
        secondary:
          "border border-border bg-elevated text-fg hover:bg-[color-mix(in_oklab,var(--surface-elevated),var(--text-primary)_6%)] active:bg-[color-mix(in_oklab,var(--surface-elevated),var(--text-primary)_14%)]",
        cta: "bg-cta text-cta-fg hover:bg-cta-hover active:bg-[color-mix(in_oklab,var(--cta-hover),var(--cta-fg)_12%)]",
        quiet: "text-muted hover:bg-hover hover:text-fg active:bg-chip",
        danger: "bg-destructive text-destructive-fg hover:bg-destructive-hover",
        "danger-quiet": "text-error hover:bg-error-bg",
        link: "h-auto rounded-none px-0 text-fg underline decoration-border underline-offset-4 hover:decoration-fg"
      },
      size: {
        sm: "h-7 px-3.5",
        md: "h-8 px-4",
        lg: "h-9 px-6",
        "icon-sm": "size-7 px-0",
        icon: "size-8 px-0",
        "icon-lg": "size-9 px-0"
      }
    },
    compoundVariants: [{ variant: "link", className: "h-auto px-0" }],
    defaultVariants: { variant: "secondary", size: "md" }
  }
);

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement>, VariantProps<typeof buttonVariants> {
  /** Render as the child element (for example an <a>) with button styling. */
  asChild?: boolean;
  /** Work in progress: shows a spinner, keeps the width and accessible name, disables clicks. */
  loading?: boolean;
}

/** Label buttons with a verb that names the result ("Publish version", not "OK"). */
export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { className, variant, size, asChild, loading, disabled, children, type, ...props },
  ref
) {
  if (asChild) {
    return (
      <Slot.Root ref={ref} className={cn(buttonVariants({ variant, size }), className)} {...props}>
        {children}
      </Slot.Root>
    );
  }
  return (
    <button
      ref={ref}
      type={type ?? "button"}
      className={cn(buttonVariants({ variant, size }), className)}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      {...props}
    >
      {loading ? (
        <>
          <span className="invisible inline-flex items-center gap-1.5">{children}</span>
          <span className="absolute inset-0 flex items-center justify-center">
            <Spinner className="size-4" />
          </span>
        </>
      ) : (
        children
      )}
    </button>
  );
});

export { buttonVariants };
