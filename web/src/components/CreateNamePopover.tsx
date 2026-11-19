import { useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Button, Dialog, DialogClose, DialogContent, Field, Input, Popover, PopoverContent, PopoverTrigger } from "../ui";

type NameFormProps = {
  inputLabel: string;
  name: string;
  onNameChange: (value: string) => void;
  onSave: () => void;
  placeholder: string;
};

function NameField({ inputLabel, name, onNameChange, placeholder }: Omit<NameFormProps, "onSave">) {
  const { t } = useTranslation();
  return (
    <Field label={t("nav.name")}>
      {(field) => (
        <Input
          {...field}
          autoFocus
          aria-label={inputLabel}
          placeholder={placeholder}
          value={name}
          onChange={(event) => onNameChange(event.target.value)}
        />
      )}
    </Field>
  );
}

/** Names a new resource (table, view, workflow, role) in a small panel under its + button. */
export function CreateNamePopover({
  trigger,
  title,
  align = "start",
  ...form
}: NameFormProps & { trigger: ReactNode; title: string; align?: "start" | "end" }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  // After a save, whatever the new resource opens (a page, an editor) takes
  // focus; returning it to the + button would close such an editor at once.
  const saved = useRef(false);
  function save() {
    saved.current = true;
    form.onSave();
    setOpen(false);
  }
  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        saved.current = false;
        setOpen(next);
      }}
    >
      <PopoverTrigger asChild>{trigger}</PopoverTrigger>
      <PopoverContent
        align={align}
        aria-label={title}
        className="w-72 p-4"
        onCloseAutoFocus={(event) => {
          if (saved.current) {
            event.preventDefault();
            saved.current = false;
          }
        }}
      >
        <form
          className="flex flex-col gap-3"
          onSubmit={(event) => {
            event.preventDefault();
            save();
          }}
        >
          <p className="text-14 font-medium text-fg">{title}</p>
          <NameField {...form} />
          <div className="flex justify-end gap-2">
            <Button size="sm" onClick={() => setOpen(false)}>
              {t("common.cancel")}
            </Button>
            <Button size="sm" variant="cta" type="submit">
              {t("common.save")}
            </Button>
          </div>
        </form>
      </PopoverContent>
    </Popover>
  );
}

/** The same form in a dialog, for entries that live in a menu. */
export function CreateNameDialog({
  open,
  onOpenChange,
  title,
  description,
  ...form
}: NameFormProps & { open: boolean; onOpenChange: (open: boolean) => void; title: string; description?: string }) {
  const { t } = useTranslation();
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={title}
        description={description}
        footer={
          <>
            <DialogClose asChild>
              <Button>{t("common.cancel")}</Button>
            </DialogClose>
            <Button
              variant="cta"
              onClick={() => {
                form.onSave();
                onOpenChange(false);
              }}
            >
              {t("common.save")}
            </Button>
          </>
        }
      >
        <form
          onSubmit={(event) => {
            event.preventDefault();
            form.onSave();
            onOpenChange(false);
          }}
        >
          <NameField {...form} />
        </form>
      </DialogContent>
    </Dialog>
  );
}
