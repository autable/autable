import { Check, ChevronDown } from "lucide-react";
import { Select as S, Switch as Sw, Tabs as Tb } from "radix-ui";
import { forwardRef, type ComponentProps, type InputHTMLAttributes, type ReactNode, type SelectHTMLAttributes } from "react";
import { cn } from "../cn";

/** Switch: the text color when on (grayscale), the thumb has no shadow. */
export function Switch({ className, ...props }: ComponentProps<typeof Sw.Root>) {
  return (
    <Sw.Root
      className={cn(
        "peer inline-flex h-5 w-8 shrink-0 cursor-pointer items-center rounded-full bg-placeholder transition-colors duration-150 disabled:cursor-not-allowed disabled:opacity-60 data-[state=checked]:bg-fg",
        className
      )}
      {...props}
    >
      <Sw.Thumb className="pointer-events-none block size-4 translate-x-0.5 rounded-full bg-elevated transition-transform duration-150 data-[state=checked]:translate-x-3.5 dark:data-[state=checked]:bg-surface" />
    </Sw.Root>
  );
}

export interface SelectOption {
  value: string;
  label: ReactNode;
  description?: ReactNode;
  disabled?: boolean;
}

/** Single-choice dropdown: a pill trigger as tall as an input. */
export function Select({
  value,
  onValueChange,
  options,
  placeholder,
  className,
  id,
  disabled,
  "aria-label": ariaLabel
}: {
  value?: string;
  onValueChange: (v: string) => void;
  options: SelectOption[];
  placeholder?: string;
  className?: string;
  id?: string;
  disabled?: boolean;
  "aria-label"?: string;
}) {
  return (
    <S.Root value={value} onValueChange={onValueChange} disabled={disabled}>
      <S.Trigger
        id={id}
        aria-label={ariaLabel}
        className={cn(
          "inline-flex h-9 w-full items-center justify-between gap-2 rounded-full border border-border bg-elevated pl-4 pr-3 text-14 text-fg transition-colors duration-150 hover:bg-[color-mix(in_oklab,var(--surface-elevated),var(--text-primary)_4%)] focus:outline-none focus-visible:border-focus focus-visible:ring-3 focus-visible:ring-focus-soft/40 disabled:opacity-60 data-[placeholder]:text-placeholder",
          className
        )}
      >
        <span className="truncate">
          <S.Value placeholder={placeholder} />
        </span>
        <S.Icon>
          <ChevronDown className="size-4 text-muted" />
        </S.Icon>
      </S.Trigger>
      <S.Portal>
        <S.Content
          position="popper"
          sideOffset={6}
          className="z-50 max-h-80 min-w-[var(--radix-select-trigger-width)] overflow-hidden rounded-container border border-border bg-elevated shadow-float animate-pop-in"
        >
          <S.Viewport className="p-1">
            {options.map((o) => (
              <S.Item
                key={o.value}
                value={o.value}
                disabled={o.disabled}
                className="relative flex cursor-default select-none flex-col justify-center rounded-inner py-1.5 pl-8 pr-2.5 text-13 text-fg outline-none data-[disabled]:opacity-50 data-[highlighted]:bg-hover"
              >
                <S.ItemIndicator className="absolute left-2.5 top-1/2 -translate-y-1/2">
                  <Check className="size-4" />
                </S.ItemIndicator>
                <S.ItemText>{o.label}</S.ItemText>
                {o.description && <span className="text-12 text-muted">{o.description}</span>}
              </S.Item>
            ))}
          </S.Viewport>
        </S.Content>
      </S.Portal>
    </S.Root>
  );
}

/**
 * Native <select> styled like Select. autable addition: used where a plain
 * form control is the better fit (dense editors, keyboard-first forms,
 * long option lists) and where scripts drive the control by value.
 */
export const NativeSelect = forwardRef<HTMLSelectElement, SelectHTMLAttributes<HTMLSelectElement> & { selectSize?: "sm" | "md" }>(
  function NativeSelect({ className, selectSize = "md", children, ...props }, ref) {
    return (
      <span className={cn("relative inline-flex w-full", className)}>
        <select
          ref={ref}
          className={cn(
            "w-full appearance-none rounded-full border border-border bg-elevated pl-3.5 pr-8 text-fg transition-[border-color,box-shadow] duration-150 focus:border-focus focus:outline-none focus:ring-3 focus:ring-focus-soft/40 disabled:cursor-not-allowed disabled:opacity-60",
            selectSize === "sm" ? "h-8 text-13" : "h-9 text-14"
          )}
          {...props}
        >
          {children}
        </select>
        <ChevronDown className="pointer-events-none absolute right-3 top-1/2 size-4 -translate-y-1/2 text-muted" aria-hidden />
      </span>
    );
  }
);

/** Checkbox with its label. autable addition. */
export const Checkbox = forwardRef<HTMLInputElement, Omit<InputHTMLAttributes<HTMLInputElement>, "type"> & { label: ReactNode }>(
  function Checkbox({ className, label, disabled, ...props }, ref) {
    return (
      <label
        className={cn(
          "inline-flex min-h-7 cursor-pointer select-none items-center gap-2 text-13 text-fg",
          disabled && "cursor-not-allowed opacity-60",
          className
        )}
      >
        <span className="relative inline-flex size-4 shrink-0">
          <input
            ref={ref}
            type="checkbox"
            disabled={disabled}
            className="peer size-4 cursor-pointer appearance-none rounded-[5px] border border-[color-mix(in_oklab,var(--border),var(--text-primary)_20%)] bg-elevated transition-colors duration-150 checked:border-fg checked:bg-fg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus-soft disabled:cursor-not-allowed"
            {...props}
          />
          <Check
            className="pointer-events-none absolute inset-0 m-auto size-3 text-surface opacity-0 peer-checked:opacity-100"
            strokeWidth={3}
            aria-hidden
          />
        </span>
        <span className="min-w-0">{label}</span>
      </label>
    );
  }
);

/** Segmented switch: gray pill track, the selected item a white pill. */
export const Tabs = Tb.Root;

export function TabsList({ className, ...props }: ComponentProps<typeof Tb.List>) {
  return <Tb.List className={cn("inline-flex h-8 items-center gap-0.5 rounded-full bg-chip p-0.5", className)} {...props} />;
}

export function TabsTrigger({ className, ...props }: ComponentProps<typeof Tb.Trigger>) {
  return (
    <Tb.Trigger
      className={cn(
        "inline-flex h-7 items-center justify-center gap-1.5 rounded-full px-3 text-13 font-medium text-muted transition-colors duration-150 hover:text-fg data-[state=active]:bg-elevated data-[state=active]:text-fg data-[state=active]:shadow-[0_1px_2px_rgb(0_0_0/0.08)] [&_svg]:size-3.5",
        className
      )}
      {...props}
    />
  );
}

export const TabsContent = Tb.Content;
