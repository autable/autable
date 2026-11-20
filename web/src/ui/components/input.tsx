import { forwardRef, useId, type InputHTMLAttributes, type LabelHTMLAttributes, type ReactNode, type TextareaHTMLAttributes } from "react";
import { cn } from "../cn";

const fieldBase =
  "w-full border border-border bg-elevated text-14 text-fg transition-[border-color,box-shadow] duration-150 placeholder:text-placeholder focus:outline-none focus:border-focus focus:ring-3 focus:ring-focus-soft/40 disabled:cursor-not-allowed disabled:opacity-60 read-only:bg-transparent aria-invalid:border-error-border aria-invalid:focus:ring-error-border/40";

/** Single-line input: a pill, 36px by default. */
export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement> & { inputSize?: "sm" | "md" | "lg" }>(
  function Input({ className, inputSize = "md", ...props }, ref) {
    const h = { sm: "h-8 px-3.5 text-13", md: "h-9 px-4", lg: "h-10 px-4" }[inputSize];
    return <input ref={ref} className={cn(fieldBase, "rounded-full", h, className)} {...props} />;
  }
);

/** Multi-line input: 8px radius (tall boxes are not pills). */
export const Textarea = forwardRef<HTMLTextAreaElement, TextareaHTMLAttributes<HTMLTextAreaElement>>(function Textarea(
  { className, ...props },
  ref
) {
  return <textarea ref={ref} className={cn(fieldBase, "min-h-20 rounded-inner px-3.5 py-2.5 leading-relaxed", className)} {...props} />;
});

export function Label({ className, ...props }: LabelHTMLAttributes<HTMLLabelElement>) {
  return <label className={cn("text-13 font-medium text-fg", className)} {...props} />;
}

/** Form field: label + control + hint or error. Errors say why and how to fix it. */
export function Field({
  label,
  hint,
  error,
  children,
  className
}: {
  label: ReactNode;
  hint?: ReactNode;
  error?: ReactNode;
  children: (props: { id: string; "aria-describedby"?: string; "aria-invalid"?: boolean }) => ReactNode;
  className?: string;
}) {
  const id = useId();
  const describedBy = error || hint ? `${id}-desc` : undefined;
  return (
    <div className={cn("flex flex-col gap-1.5", className)}>
      <Label htmlFor={id}>{label}</Label>
      {children({ id, "aria-describedby": describedBy, "aria-invalid": error ? true : undefined })}
      {(error || hint) && (
        <p id={describedBy} className={cn("text-12", error ? "text-error" : "text-muted")}>
          {error ?? hint}
        </p>
      )}
    </div>
  );
}
