import { Menu } from "lucide-react";
import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Button, Dialog, DialogTrigger, SheetContent } from "../ui";
import { LogoMark } from "./Logo";
import { useIsDesktop } from "./media";
import { Sidebar, UserMenu, type SidebarProps } from "./Sidebar";

/**
 * The app frame. Desktop (md and up): a fixed sidebar with the database,
 * its tables and, for admins, the Manage group; pages own their headers.
 * Mobile: a top bar with the page title, the same sidebar in a drawer, and
 * the account menu.
 */
export function AppShell({ sidebar, title, children }: { sidebar: SidebarProps; title: string; children: ReactNode }) {
  const { t } = useTranslation();
  const desktop = useIsDesktop();
  const [drawerOpen, setDrawerOpen] = useState(false);
  return (
    <div className="flex h-full">
      {desktop && (
        <aside className="flex w-64 shrink-0 flex-col border-r border-border-soft bg-sidebar">
          <Sidebar {...sidebar} />
        </aside>
      )}
      <div className="flex min-w-0 flex-1 flex-col">
        {!desktop && (
          <header className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-2">
            <Dialog open={drawerOpen} onOpenChange={setDrawerOpen}>
              <DialogTrigger asChild>
                <Button variant="quiet" size="icon-sm" aria-label={t("nav.openNavigation")}>
                  <Menu />
                </Button>
              </DialogTrigger>
              <SheetContent title={t("nav.navigation")}>
                <Sidebar {...sidebar} onNavigated={() => setDrawerOpen(false)} />
              </SheetContent>
            </Dialog>
            <LogoMark className="size-5" />
            <span className="min-w-0 flex-1 truncate text-14 font-medium text-fg">{title}</span>
            <UserMenu {...sidebar} compact />
          </header>
        )}
        <main className="min-h-0 flex-1 overflow-y-auto">{children}</main>
      </div>
    </div>
  );
}
