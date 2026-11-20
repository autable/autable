import { AppWindow, ChevronRight, Server, Table2, Users, Workflow, type LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { AppDefinition, DatabaseMetadata, RoleDefinition, WorkflowDefinition } from "../api";
import type { WorkspaceRoute } from "../appState";
import { Page, SectionHeading } from "../shell/Page";
import { RouteLink } from "../shell/navigation";
import { tableRoute } from "../shell/Sidebar";
import { Card, cn, Table, TBody, Td, Th, THead, Tr } from "../ui";

/**
 * The admin overview of one database: every management area with its
 * current size, then the tables with their schema at a glance. Admins only.
 */
export function AdminPage({
  apps,
  database,
  onOpenRunners,
  roles,
  workflows
}: {
  apps: AppDefinition[];
  database: DatabaseMetadata;
  onOpenRunners: () => void;
  roles: RoleDefinition[];
  workflows: WorkflowDefinition[];
}) {
  const { t } = useTranslation();
  const db = database.name;
  const enabledWorkflows = workflows.filter((workflow) => workflow.enabled && workflow.published_version > 0).length;
  const failingWorkflows = workflows.filter((workflow) => workflow.last_run?.status === "failed").length;
  const workflowsMeta = t("admin.workflowsMeta", { workflows: t("admin.workflowCount", { count: workflows.length }), enabled: enabledWorkflows });
  const publishedApps = apps.filter((app) => app.published_version > 0).length;
  return (
    <Page title={t("admin.title")} description={t("admin.description", { database: db })}>
      <section aria-labelledby="admin-areas" className="mb-10">
        <SectionHeading id="admin-areas" title={t("admin.areas")} />
        <div className="grid gap-3 md:grid-cols-2">
          <AreaCard
            icon={Workflow}
            title={t("admin.workflowsTitle")}
            description={t("admin.workflowsDescription")}
            meta={failingWorkflows > 0 ? `${workflowsMeta} · ${t("admin.workflowsFailing", { count: failingWorkflows })}` : workflowsMeta}
            route={{ databaseName: db, view: "workflow" }}
          />
          <AreaCard
            icon={Users}
            title={t("admin.permissionsTitle")}
            description={t("admin.permissionsDescription")}
            meta={t("admin.permissionsMeta", { count: roles.length })}
            route={{ databaseName: db, view: "permission" }}
          />
          <AreaCard
            icon={AppWindow}
            title={t("admin.appsTitle")}
            description={t("admin.appsDescription")}
            meta={t("admin.appsMeta", { apps: t("admin.appCount", { count: apps.length }), published: publishedApps })}
            route={{ databaseName: db, view: "app" }}
          />
          <AreaCard icon={Server} title={t("admin.runnersTitle")} description={t("admin.runnersDescription")} onClick={onOpenRunners} />
        </div>
      </section>
      <section aria-labelledby="admin-tables">
        <SectionHeading
          id="admin-tables"
          title={t("admin.tablesTitle")}
          action={<span className="text-12 text-subtle">{t("admin.tablesMeta", { count: database.tables.length })}</span>}
        />
        <Card className="overflow-hidden">
          {database.tables.length === 0 ? (
            <p className="px-5 py-6 text-center text-13 text-muted">{t("home.noTablesAdmin")}</p>
          ) : (
            <Table aria-label={t("admin.tablesTitle")}>
              <THead>
                <tr>
                  <Th>{t("admin.tableColumns.table")}</Th>
                  <Th className="text-right">{t("admin.tableColumns.fields")}</Th>
                  <Th className="hidden text-right sm:table-cell">{t("admin.tableColumns.formulas")}</Th>
                  <Th className="text-right">{t("admin.tableColumns.views")}</Th>
                  <Th className="w-10" />
                </tr>
              </THead>
              <TBody>
                {database.tables.map((table) => {
                  const fields = table.fields.filter((field) => !field.deleted);
                  return (
                    <Tr key={table.name} className="relative">
                      <Td>
                        <RouteLink
                          route={tableRoute(db, table)}
                          className="flex min-w-0 items-center gap-3 font-medium text-fg after:absolute after:inset-0"
                        >
                          <Table2 className="size-4 shrink-0 text-muted" strokeWidth={1.75} aria-hidden />
                          <span className="truncate">{table.display_name || table.name}</span>
                          {table.display_name && table.display_name !== table.name && (
                            <code className="hidden truncate font-mono text-12 font-normal text-subtle sm:inline">{table.name}</code>
                          )}
                        </RouteLink>
                      </Td>
                      <Td className="text-right tabular-nums text-description">{fields.length}</Td>
                      <Td className="hidden text-right tabular-nums text-description sm:table-cell">
                        {fields.filter((field) => field.type === "formula").length}
                      </Td>
                      <Td className="text-right tabular-nums text-description">{(table.views ?? []).length}</Td>
                      <Td className="text-right">
                        <ChevronRight className="ml-auto size-4 text-subtle" aria-hidden />
                      </Td>
                    </Tr>
                  );
                })}
              </TBody>
            </Table>
          )}
        </Card>
      </section>
    </Page>
  );
}

function AreaCard({
  icon: Icon,
  title,
  description,
  meta,
  route,
  onClick
}: {
  icon: LucideIcon;
  title: string;
  description: string;
  meta?: string;
  route?: WorkspaceRoute;
  onClick?: () => void;
}) {
  const className = "flex w-full items-start gap-3.5 px-5 py-4 text-left after:absolute after:inset-0 after:rounded-container";
  const body: ReactNode = (
    <>
      <span className="flex size-9 shrink-0 items-center justify-center rounded-inner bg-chip text-description" aria-hidden>
        <Icon className="size-4.5" strokeWidth={1.75} />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block text-14 font-medium text-fg">{title}</span>
        <span className="mt-0.5 block text-13 text-muted">{description}</span>
        {meta && <span className="mt-2 block text-12 tabular-nums text-subtle">{meta}</span>}
      </span>
      <ChevronRight className="mt-2.5 size-4 shrink-0 text-subtle" aria-hidden />
    </>
  );
  return (
    <Card className={cn("relative transition-colors duration-150 hover:bg-[color-mix(in_oklab,var(--surface-elevated),var(--text-primary)_3%)]")}>
      {route ? (
        <RouteLink route={route} className={className}>
          {body}
        </RouteLink>
      ) : (
        <button type="button" className={className} onClick={onClick}>
          {body}
        </button>
      )}
    </Card>
  );
}
