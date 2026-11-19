import { DropdownMenu as M, Tooltip as T } from "radix-ui";
import type { ComponentProps, ReactNode } from "react";
import { cn } from "../cn";

export const DropdownMenu = M.Root;
export const DropdownMenuTrigger = M.Trigger;
export const DropdownMenuRadioGroup = M.RadioGroup;

export function DropdownMenuContent({ className, align = "end", ...props }: ComponentProps<typeof M.Content>) {
  return (
    <M.Portal>
      <M.Content
        align={align}
        sideOffset={6}
        collisionPadding={8}
        className={cn("z-50 min-w-44 rounded-container border border-border bg-elevated p-1 shadow-float animate-pop-in", className)}
        {...props}
      />
    </M.Portal>
  );
}

const itemClass =
  "flex h-8 cursor-default select-none items-center gap-2 rounded-inner px-2.5 text-13 text-fg outline-none data-[disabled]:pointer-events-none data-[disabled]:opacity-50 data-[highlighted]:bg-hover [&_svg]:size-4 [&_svg]:text-muted";

export function DropdownMenuItem({ className, danger, ...props }: ComponentProps<typeof M.Item> & { danger?: boolean }) {
  return (
    <M.Item
      className={cn(itemClass, danger && "text-error data-[highlighted]:bg-error-bg [&_svg]:text-current", className)}
      {...props}
    />
  );
}

/** A checked choice inside a DropdownMenuRadioGroup. autable addition. */
export function DropdownMenuRadioItem({ className, children, ...props }: ComponentProps<typeof M.RadioItem>) {
  return (
    <M.RadioItem className={cn(itemClass, "pr-8 relative", className)} {...props}>
      {children}
      <M.ItemIndicator className="absolute right-2.5 inline-flex">
        <span className="size-1.5 rounded-full bg-fg" />
      </M.ItemIndicator>
    </M.RadioItem>
  );
}

export function DropdownMenuLabel({ className, ...props }: ComponentProps<typeof M.Label>) {
  return <M.Label className={cn("px-2.5 py-1.5 text-12 text-muted", className)} {...props} />;
}

export function DropdownMenuSeparator({ className, ...props }: ComponentProps<typeof M.Separator>) {
  return <M.Separator className={cn("-mx-1 my-1 h-px bg-border-soft", className)} {...props} />;
}

export const TooltipProvider = T.Provider;

/** Tooltip: icon-only buttons always get a text explanation. */
export function Tooltip({
  content,
  children,
  side = "top"
}: {
  content: ReactNode;
  children: ReactNode;
  side?: "top" | "bottom" | "left" | "right";
}) {
  return (
    <T.Root delayDuration={400}>
      <T.Trigger asChild>{children}</T.Trigger>
      <T.Portal>
        <T.Content
          side={side}
          sideOffset={6}
          className="z-50 max-w-xs rounded-inner bg-cta px-2 py-1 text-12 text-cta-fg shadow-float animate-fade-in"
        >
          {content}
        </T.Content>
      </T.Portal>
    </T.Root>
  );
}
