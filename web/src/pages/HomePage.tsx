import { ArrowUpRight, ChevronRight, Inbox, Table2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { AppDefinition, DatabaseMetadata } from "../api";
import { AppTile, PublishedStatus } from "../components/AppWorkspace";
import { Page, SectionHeading } from "../shell/Page";
import { RouteLink } from "../shell/navigation";
import { tableRoute } from "../shell/Sidebar";
import { Button, Card, EmptyState, Skeleton } from "../ui";

/**
 * The database landing page, for everyone: the apps the user can open, as
 * launcher cards, then the tables they can see. Admins see unpublished apps
 * and empty sections too, with a hint at how to fill them.
 */
export function HomePage({
  apps,
  appsReady,
  database,
  isAdmin
}: {
  apps: AppDefinition[];
  appsReady: boolean;
  database: DatabaseMetadata;
  isAdmin: boolean;
}) {
  const { t } = useTranslation();
  const db = database.name;
  // Ordinary users only get apps they can open; unpublished ones are noise.
  const visibleApps = isAdmin ? apps : apps.filter((app) => app.published_version > 0);
  const nothingShared = !isAdmin && appsReady && visibleApps.length === 0 && database.tables.length === 0;

  if (nothingShared) {
    return (
      <Page title={db} description={t("home.description", { database: db })}>
        <Card>
          <EmptyState icon={Inbox} title={t("home.emptyTitle")} description={t("home.emptyDescription")} />
        </Card>
      </Page>
    );
  }

  return (
    <Page title={db} description={t("home.description", { database: db })}>
      {(isAdmin || !appsReady || visibleApps.length > 0) && (
        <section aria-labelledby="home-apps" className="mb-10">
          <SectionHeading
            id="home-apps"
            title={t("home.apps")}
            action={
              visibleApps.length > 0 && (
                <Button asChild variant="quiet" size="sm" className="-mr-2">
                  <RouteLink route={{ databaseName: db, view: "app" }}>
                    {t("home.allApps")}
                    <ChevronRight aria-hidden />
                  </RouteLink>
                </Button>
              )
            }
          />
          {!appsReady ? (
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              <Skeleton className="h-[7.5rem]" />
              <Skeleton className="h-[7.5rem]" />
              <Skeleton className="h-[7.5rem]" />
            </div>
          ) : visibleApps.length === 0 ? (
            <Card className="px-5 py-4 text-13 text-muted">{t("home.noAppsAdmin")}</Card>
          ) : (
            <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {visibleApps.map((app) => (
                <li key={app.id}>
                  <AppLauncher app={app} databaseName={db} isAdmin={isAdmin} />
                </li>
              ))}
            </ul>
          )}
        </section>
      )}
      <section aria-labelledby="home-tables">
        <SectionHeading id="home-tables" title={t("home.tables")} />
        {database.tables.length === 0 ? (
          <Card className="px-5 py-4 text-13 text-muted">{isAdmin ? t("home.noTablesAdmin") : t("empty.noGrantedTableDescription")}</Card>
        ) : (
          <Card className="divide-y divide-border-soft overflow-hidden">
            {database.tables.map((table) => {
              const fields = table.fields.filter((field) => !field.deleted).length;
              const views = (table.views ?? []).length;
              return (
                <RouteLink
                  key={table.name}
                  route={tableRoute(db, table)}
                  className="flex items-center gap-3 px-5 py-3.5 transition-colors duration-150 hover:bg-hover"
                >
                  <span className="flex size-9 shrink-0 items-center justify-center rounded-inner bg-chip text-description" aria-hidden>
                    <Table2 className="size-4" strokeWidth={1.75} />
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-14 font-medium text-fg">{table.display_name || table.name}</span>
                    <span className="block truncate text-12 text-muted">
                      {t("home.tableMeta", { fields: t("home.fieldCount", { count: fields }), views: t("home.viewCount", { count: views }) })}
                    </span>
                  </span>
                  <ChevronRight className="size-4 shrink-0 text-subtle" aria-hidden />
                </RouteLink>
              );
            })}
          </Card>
        )}
      </section>
    </Page>
  );
}

function formatDay(timestamp: number): string {
  const parsed = new Date(timestamp);
  return Number.isNaN(parsed.getTime()) ? "" : parsed.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

/**
 * One app as a launcher card: the card opens the app's page here, the Open
 * button launches the published app in a new tab.
 */
function AppLauncher({ app, databaseName, isAdmin }: { app: AppDefinition; databaseName: string; isAdmin: boolean }) {
  const { t } = useTranslation();
  const title = app.title || app.name;
  const published = app.published_version > 0;
  return (
    <Card className="group relative flex h-full flex-col gap-4 p-4 transition-colors duration-150 hover:bg-[color-mix(in_oklab,var(--surface-elevated),var(--text-primary)_3%)]">
      <div className="flex items-start gap-3">
        <AppTile app={app} size="lg" />
        <div className="min-w-0 flex-1">
          <RouteLink
            route={{ databaseName, view: "app", appName: app.name }}
            className="block truncate text-14 font-medium text-fg after:absolute after:inset-0 after:rounded-container"
          >
            {title}
          </RouteLink>
          <p className="truncate font-mono text-12 text-subtle">{app.name}</p>
        </div>
      </div>
      <div className="mt-auto flex items-center justify-between gap-2">
        {isAdmin ? (
          <PublishedStatus version={app.published_version} />
        ) : (
          <span className="truncate text-12 text-subtle">{t("home.updatedOn", { date: formatDay(app.updated_at) })}</span>
        )}
        {published && (
          <Button asChild size="sm" className="relative z-10">
            <a href={app.url} target="_blank" rel="noopener" aria-label={t("app.openApp", { name: title })} title={t("home.openInNewTab")}>
              {t("app.open")}
              <ArrowUpRight aria-hidden />
            </a>
          </Button>
        )}
      </div>
    </Card>
  );
}
