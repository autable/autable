import { X } from "lucide-react";
import { Dialog as D } from "radix-ui";
import { useState, type ComponentProps, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "../cn";
import { Button } from "./button";

export const Dialog = D.Root;
export const DialogTrigger = D.Trigger;
export const DialogClose = D.Close;

type ContentProps = Omit<ComponentProps<typeof D.Content>, "title" | "children" | "className">;

/** Dialog: floating layers are one of the few places with a shadow. */
export function DialogContent({
  title,
  description,
  children,
  footer,
  className,
  bodyClassName,
  ...props
}: ContentProps & {
  title: ReactNode;
  description?: ReactNode;
  children?: ReactNode;
  footer?: ReactNode;
  className?: string;
  /** Classes for the scrolling body, e.g. to let a two-pane layout fill the dialog. */
  bodyClassName?: string;
}) {
  const { t } = useTranslation();
  return (
    <D.Portal>
      <D.Overlay className="fixed inset-0 z-50 bg-scrim animate-fade-in" />
      <D.Content
        className={cn(
          "fixed left-1/2 top-1/2 z-50 flex max-h-[85vh] w-[calc(100vw-2rem)] max-w-md -translate-x-1/2 -translate-y-1/2 flex-col rounded-container border border-border bg-elevated shadow-float animate-pop-in focus:outline-none",
          className
        )}
        {...props}
      >
        <div className="flex items-start justify-between gap-4 px-5 pt-5">
          <div className="flex min-w-0 flex-col gap-1">
            <D.Title className="text-18 font-medium text-fg">{title}</D.Title>
            {description ? (
              <D.Description className="text-13 text-muted">{description}</D.Description>
            ) : (
              <D.Description className="sr-only">{typeof title === "string" ? title : ""}</D.Description>
            )}
          </div>
          <D.Close asChild>
            <Button variant="quiet" size="icon-sm" aria-label={t("common.close")} className="-mr-1.5 -mt-0.5">
              <X />
            </Button>
          </D.Close>
        </div>
        {children && <div className={cn("min-h-0 overflow-y-auto px-5 pb-1 pt-4", bodyClassName)}>{children}</div>}
        {footer && <div className="flex justify-end gap-2 px-5 pb-5 pt-4">{footer}</div>}
      </D.Content>
    </D.Portal>
  );
}

/**
 * Side sheet: the navigation drawer on narrow screens. autable addition,
 * built on the same dialog primitive so focus and Escape behave the same.
 */
export function SheetContent({ title, children, className }: { title: string; children: ReactNode; className?: string }) {
  return (
    <D.Portal>
      <D.Overlay className="fixed inset-0 z-50 bg-scrim animate-fade-in" />
      <D.Content
        className={cn(
          "fixed inset-y-0 left-0 z-50 flex w-[min(20rem,calc(100vw-3rem))] flex-col bg-sidebar shadow-float animate-slide-in-left focus:outline-none",
          className
        )}
      >
        <D.Title className="sr-only">{title}</D.Title>
        <D.Description className="sr-only">{title}</D.Description>
        {children}
      </D.Content>
    </D.Portal>
  );
}

/**
 * Confirmation for destructive actions: the title says what will happen, the
 * body explains the effect. confirmLabel is a verb naming the result.
 */
export function ConfirmDialog({
  trigger,
  title,
  description,
  confirmLabel,
  onConfirm,
  danger = true
}: {
  trigger: ReactNode;
  title: string;
  description: ReactNode;
  confirmLabel: string;
  onConfirm: () => Promise<unknown> | void;
  danger?: boolean;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!busy) {
          setOpen(next);
          setError(null);
        }
      }}
    >
      <DialogTrigger asChild>{trigger}</DialogTrigger>
      <DialogContent
        title={title}
        description={description}
        footer={
          <>
            <DialogClose asChild>
              <Button disabled={busy}>{t("common.cancel")}</Button>
            </DialogClose>
            <Button
              variant={danger ? "danger" : "cta"}
              loading={busy}
              onClick={async () => {
                setBusy(true);
                setError(null);
                try {
                  await onConfirm();
                  setOpen(false);
                } catch (e) {
                  setError(e instanceof Error ? e.message : String(e));
                } finally {
                  setBusy(false);
                }
              }}
            >
              {confirmLabel}
            </Button>
          </>
        }
      >
        {error && <p className="text-13 text-error">{error}</p>}
      </DialogContent>
    </Dialog>
  );
}
