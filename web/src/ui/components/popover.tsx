import { Popover as P } from "radix-ui";
import type { ComponentProps } from "react";
import { cn } from "../cn";

export const Popover = P.Root;
export const PopoverTrigger = P.Trigger;
export const PopoverAnchor = P.Anchor;
export const PopoverClose = P.Close;

/** Popover: a panel anchored to a control. Floating layers may have a shadow. */
export function PopoverContent({ className, align = "start", sideOffset = 6, ...props }: ComponentProps<typeof P.Content>) {
  return (
    <P.Portal>
      <P.Content
        align={align}
        sideOffset={sideOffset}
        collisionPadding={8}
        className={cn(
          "z-50 max-h-[var(--radix-popover-content-available-height)] w-72 max-w-[calc(100vw-1rem)] overflow-auto rounded-container border border-border bg-elevated shadow-float animate-pop-in focus:outline-none",
          className
        )}
        {...props}
      />
    </P.Portal>
  );
}
