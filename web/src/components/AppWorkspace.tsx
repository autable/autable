import { AppWindow, ArrowLeft, Download, Eye, ExternalLink, Rocket, Trash2, Undo2 } from "lucide-react";
import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { appSourceURL, type AppDefinition, type AppDetail, type AppVersion, type AuthUser } from "../api";
import { Page, SectionHeading } from "../shell/Page";
import { RouteLink } from "../shell/navigation";
import {
  Badge,
  Button,
  Card,
  cn,
  Dialog,
  DialogClose,
  DialogContent,
  EmptyState,
  Skeleton,
  StatusBadge,
  Table,
  TBody,
  Td,
  Th,
  THead,
  Tooltip,
  Tr
} from "../ui";

type AppWorkspaceProps = {
  apps: AppDefinition[];
  appsReady: boolean;
  busy: boolean;
  currentUser: AuthUser | null;
  databaseName: string;
  detail: AppDetail | null;
  // Publishing, rollback, previews, sources and deletion are admin-only;
  // everyone else can only open the apps they are granted.
  isAdmin: boolean;
  onDelete: () => void;
  onPublish: (version: number) => void;
  onSelectApp: (name: string) => void;
  selectedAppName: string;
};

export function AppWorkspace(props: AppWorkspaceProps) {
  if (props.selectedAppName) {
    return props.detail ? (
      <AppDetailView
        busy={props.busy}
        currentUser={props.currentUser}
        databaseName={props.databaseName}
        detail={props.detail}
        isAdmin={props.isAdmin}
        onDelete={props.onDelete}
        onPublish={props.onPublish}
      />
    ) : (
      <AppLoading />
    );
  }
  if (!props.appsReady) {
    return <AppLoading />;
  }
  return <AppListView apps={props.apps} databaseName={props.databaseName} isAdmin={props.isAdmin} onSelectApp={props.onSelectApp} />;
}

function AppLoading() {
  const { t } = useTranslation();
  return (
    <Page title={t("app.apps")}>
      <div role="status" aria-label={t("app.loading")} className="flex flex-col gap-3">
        <Skeleton className="h-11" />
        <Skeleton className="h-11" />
        <Skeleton className="h-11" />
      </div>
    </Page>
  );
}

/** A square tile with the app's initial, used wherever an app is listed. */
export function AppTile({ app, size = "md" }: { app: AppDefinition; size?: "md" | "lg" }) {
  const label = Array.from((app.title || app.name).trim())[0]?.toUpperCase() ?? "?";
  return (
    <span
      aria-hidden
      className={cn(
        "flex shrink-0 items-center justify-center rounded-inner bg-chip font-medium text-description",
        size === "lg" ? "size-10 text-15" : "size-8 text-13"
      )}
    >
      {label}
    </span>
  );
}

export function PublishedStatus({ version }: { version: number }) {
  const { t } = useTranslation();
  return version > 0 ? (
    <StatusBadge tone="success" label={t("app.versionLabel", { version })} />
  ) : (
    <StatusBadge tone="neutral" label={t("app.notPublished")} />
  );
}

function AppListView({
  apps,
  databaseName,
  isAdmin,
  onSelectApp
}: {
  apps: AppDefinition[];
  databaseName: string;
  isAdmin: boolean;
  onSelectApp: (name: string) => void;
}) {
  const { t } = useTranslation();
  return (
    <Page title={t("app.apps")} description={t("app.listLabel", { database: databaseName })}>
      {apps.length === 0 ? (
        <Card>
          <EmptyState
            icon={AppWindow}
            title={t("app.emptyTitle")}
            description={t("app.emptyDescription")}
            action={isAdmin ? <AppCommandHelp databaseName={databaseName} /> : undefined}
          />
        </Card>
      ) : (
        <>
          <Card className="overflow-hidden">
            <Table aria-label={t("app.appTable")}>
              <THead>
                <tr>
                  <Th>{t("app.title")}</Th>
                  <Th className="hidden sm:table-cell">{t("app.name")}</Th>
                  <Th>{t("app.publishedVersion")}</Th>
                  <Th className="hidden md:table-cell">{t("app.updated")}</Th>
                  <Th className="text-right">
                    <span className="sr-only">{t("app.actions")}</span>
                  </Th>
                </tr>
              </THead>
              <TBody>
                {apps.map((app) => (
                  <Tr key={app.id}>
                    <Td className="py-2">
                      <span className="flex min-w-0 items-center gap-3">
                        <AppTile app={app} />
                        <button
                          type="button"
                          className="min-w-0 truncate text-left text-14 font-medium text-fg underline-offset-4 hover:underline"
                          onClick={() => onSelectApp(app.name)}
                        >
                          {app.title || app.name}
                        </button>
                      </span>
                    </Td>
                    <Td className="hidden sm:table-cell">
                      <code className="font-mono text-12 text-muted">{app.name}</code>
                    </Td>
                    <Td>
                      <PublishedStatus version={app.published_version} />
                    </Td>
                    <Td className="hidden whitespace-nowrap text-muted md:table-cell">{formatTime(app.updated_at)}</Td>
                    <Td className="text-right">
                      <OpenAppButton app={app} />
                    </Td>
                  </Tr>
                ))}
              </TBody>
            </Table>
          </Card>
          {isAdmin && (
            <div className="mt-8">
              <SectionHeading title={t("app.howToTitle")} />
              <Card className="px-5 py-4">
                <AppCommandHelp databaseName={databaseName} />
              </Card>
            </div>
          )}
        </>
      )}
    </Page>
  );
}

function AppCommandHelp({ databaseName }: { databaseName: string }) {
  const { t } = useTranslation();
  return (
    <div className="flex w-full max-w-xl flex-col gap-2 text-left">
      <CommandStep label={t("app.stepInit")} command={`autablectl app init my-app --database ${databaseName} --name my-app`} />
      <CommandStep label={t("app.stepPush")} command={"cd my-app\nautablectl app push --publish"} />
      <CommandStep label={t("app.skillHint")} command="autablectl skill install" />
      <CommandStep label={t("app.skillExportHint")} command="autablectl skill export ./autable-skill" />
    </div>
  );
}

function CommandStep({ label, command }: { label: string; command: string }) {
  return (
    <div className="flex flex-col gap-1">
      <p className="text-13 text-muted">{label}</p>
      <pre className="overflow-x-auto rounded-inner bg-chip px-3 py-2 font-mono text-12 leading-relaxed text-fg">{command}</pre>
    </div>
  );
}

function OpenAppButton({ app, primary = false }: { app: AppDefinition; primary?: boolean }) {
  const { t } = useTranslation();
  const variant = primary ? "cta" : "secondary";
  const size = primary ? "md" : "sm";
  if (app.published_version <= 0) {
    return (
      <Button variant={variant} size={size} disabled>
        <ExternalLink aria-hidden />
        {t("app.open")}
      </Button>
    );
  }
  return (
    <Button asChild variant={variant} size={size}>
      <a href={app.url} target="_blank" rel="noopener" aria-label={t("app.openApp", { name: app.title || app.name })}>
        <ExternalLink aria-hidden />
        {t("app.open")}
      </a>
    </Button>
  );
}

type PendingAction = { kind: "rollback"; version: number } | { kind: "delete" } | null;

function AppDetailView({
  busy,
  currentUser,
  databaseName,
  detail,
  isAdmin,
  onDelete,
  onPublish
}: {
  busy: boolean;
  currentUser: AuthUser | null;
  databaseName: string;
  detail: AppDetail;
  isAdmin: boolean;
  onDelete: () => void;
  onPublish: (version: number) => void;
}) {
  const { t } = useTranslation();
  const [pending, setPending] = useState<PendingAction>(null);
  const { app, sdk } = detail;
  const versions = [...detail.versions].sort((left, right) => right.version - left.version);
  const latest = versions[0]?.version ?? app.latest_version;

  function requestPublish(version: AppVersion) {
    if (app.published_version > version.version) {
      setPending({ kind: "rollback", version: version.version });
      return;
    }
    onPublish(version.version);
  }

  function confirmPending() {
    if (pending?.kind === "rollback") {
      onPublish(pending.version);
    } else if (pending?.kind === "delete") {
      onDelete();
    }
    setPending(null);
  }

  return (
    <Page
      width="wide"
      title={app.title || app.name}
      description={t("app.detailLabel", { name: app.name, database: databaseName })}
      icon={<AppTile app={app} size="lg" />}
      back={
        <Button asChild variant="quiet" size="sm" className="-ml-2.5">
          <RouteLink route={{ databaseName, view: "app" }}>
            <ArrowLeft aria-hidden />
            {t("app.backToApps")}
          </RouteLink>
        </Button>
      }
      actions={
        <>
          {isAdmin && (
            <Button variant="danger-quiet" disabled={busy} onClick={() => setPending({ kind: "delete" })}>
              <Trash2 aria-hidden />
              {t("app.deleteApp")}
            </Button>
          )}
          <OpenAppButton app={app} primary />
        </>
      }
    >
      <section aria-labelledby="app-about-heading" className="mb-8">
        <h2 id="app-about-heading" className="sr-only">
          {t("app.about")}
        </h2>
        <Card>
          <dl className="grid grid-cols-2 gap-x-6 gap-y-4 px-5 py-4 text-13 md:grid-cols-4">
            <AboutItem label={t("app.publishedVersion")}>
              <PublishedStatus version={app.published_version} />
            </AboutItem>
            {isAdmin && (
              <AboutItem label={t("app.latestVersion")}>{latest ? t("app.versionLabel", { version: latest }) : t("app.none")}</AboutItem>
            )}
            <AboutItem label={t("app.updated")}>{formatTime(app.updated_at)}</AboutItem>
            <AboutItem label={t("app.address")} className={isAdmin ? undefined : "col-span-2"}>
              <span className="break-all font-mono text-12 text-description">{app.url}</span>
            </AboutItem>
          </dl>
        </Card>
      </section>
      {isAdmin && (
          <section aria-labelledby="app-versions-heading" className="min-w-0">
            <SectionHeading
              id="app-versions-heading"
              title={t("app.versions")}
              action={<span className="text-12 text-subtle">{t("app.serverSDK", { version: sdk.version, min: sdk.min_major })}</span>}
            />
            <Card className="overflow-hidden">
              {versions.length === 0 ? (
                <p className="px-5 py-8 text-center text-13 text-muted">{t("app.noVersions")}</p>
              ) : (
                <Table aria-label={t("app.versionTable")}>
                  <THead>
                    <tr>
                      <Th>{t("app.version")}</Th>
                      <Th>{t("app.message")}</Th>
                      <Th className="hidden lg:table-cell">{t("app.sdk")}</Th>
                      <Th className="hidden lg:table-cell">{t("app.uploadedBy")}</Th>
                      <Th className="hidden md:table-cell">{t("app.uploadedAt")}</Th>
                      <Th className="hidden lg:table-cell">{t("app.size")}</Th>
                      <Th className="text-right">
                        <span className="sr-only">{t("app.actions")}</span>
                      </Th>
                    </tr>
                  </THead>
                  <TBody>
                    {versions.map((version) => (
                      <VersionRow
                        key={version.version}
                        app={app}
                        busy={busy}
                        currentUser={currentUser}
                        databaseName={databaseName}
                        onPublish={requestPublish}
                        version={version}
                      />
                    ))}
                  </TBody>
                </Table>
              )}
            </Card>
          </section>
      )}
      <Dialog open={pending !== null} onOpenChange={(open) => !open && setPending(null)}>
        <DialogContent
          title={pending?.kind === "delete" ? t("app.deleteTitle") : t("app.rollbackTitle")}
          description={
            pending?.kind === "delete"
              ? t("app.deleteConfirm", { name: app.title || app.name })
              : t("app.rollbackConfirm", {
                  current: app.published_version,
                  version: pending?.kind === "rollback" ? pending.version : 0
                })
          }
          footer={
            <>
              <DialogClose asChild>
                <Button>{t("common.cancel")}</Button>
              </DialogClose>
              <Button variant={pending?.kind === "delete" ? "danger" : "cta"} onClick={confirmPending}>
                {pending?.kind === "delete" ? t("common.delete") : t("app.rollback")}
              </Button>
            </>
          }
        />
      </Dialog>
    </Page>
  );
}

function AboutItem({ label, children, className }: { label: string; children: ReactNode; className?: string }) {
  return (
    <div className={cn("flex min-w-0 flex-col gap-1", className)}>
      <dt className="text-12 text-muted">{label}</dt>
      <dd className="min-w-0 text-fg">{children}</dd>
    </div>
  );
}

function VersionRow({
  app,
  busy,
  currentUser,
  databaseName,
  onPublish,
  version
}: {
  app: AppDefinition;
  busy: boolean;
  currentUser: AuthUser | null;
  databaseName: string;
  onPublish: (version: AppVersion) => void;
  version: AppVersion;
}) {
  const { t } = useTranslation();
  const published = version.version === app.published_version;
  const rollback = app.published_version > version.version;
  const preview = t("app.previewVersion", { version: version.version });
  const source = t("app.downloadSourceVersion", { version: version.version });
  return (
    <Tr aria-label={t("app.versionLabel", { version: version.version })}>
      <Td className="whitespace-nowrap">
        <span className="flex items-center gap-2">
          <span className={cn("tabular-nums", published ? "font-medium text-fg" : "text-description")}>
            {t("app.versionLabel", { version: version.version })}
          </span>
          {published && <StatusBadge tone="success" label={t("app.published")} />}
        </span>
      </Td>
      <Td className="min-w-48 max-w-96">
        {version.message ? (
          <span className="line-clamp-2">{version.message}</span>
        ) : (
          <span className="text-subtle">{t("app.noMessage")}</span>
        )}
      </Td>
      <Td className="hidden whitespace-nowrap lg:table-cell">
        <span className="flex items-center gap-2">
          <code className="font-mono text-12">{version.sdk}</code>
          {!version.supported && (
            <Tooltip content={version.sdk_error ?? t("app.unsupported")}>
              <Badge tabIndex={0} className="bg-error-bg text-error">
                {t("app.unsupported")}
              </Badge>
            </Tooltip>
          )}
        </span>
      </Td>
      <Td className="hidden max-w-40 lg:table-cell">
        <span className="block truncate text-description" title={version.created_by}>
          {version.created_by === currentUser?.id ? currentUser.display_name : version.created_by}
        </span>
      </Td>
      <Td className="hidden whitespace-nowrap text-muted md:table-cell">{formatTime(version.created_at)}</Td>
      <Td className="hidden whitespace-nowrap tabular-nums text-muted lg:table-cell">{formatSize(version.size)}</Td>
      <Td className="whitespace-nowrap text-right">
        <span className="inline-flex items-center justify-end gap-0.5">
          <Tooltip content={preview}>
            <Button asChild variant="quiet" size="icon-sm">
              <a href={version.preview_url} target="_blank" rel="noopener" aria-label={preview}>
                <Eye aria-hidden />
              </a>
            </Button>
          </Tooltip>
          <Tooltip content={source}>
            <Button asChild variant="quiet" size="icon-sm">
              <a href={appSourceURL(databaseName, app.name, version.version)} download aria-label={source}>
                <Download aria-hidden />
              </a>
            </Button>
          </Tooltip>
          {!published && (
            <Button
              size="sm"
              variant={rollback ? "secondary" : "primary"}
              className="ml-1"
              disabled={busy || !version.supported}
              aria-label={t(rollback ? "app.rollbackVersion" : "app.publishVersion", { version: version.version })}
              onClick={() => onPublish(version)}
            >
              {rollback ? <Undo2 aria-hidden /> : <Rocket aria-hidden />}
              {t(rollback ? "app.rollback" : "app.publish")}
            </Button>
          )}
        </span>
      </Td>
    </Tr>
  );
}

function formatTime(timestamp: number): string {
  const parsed = new Date(timestamp);
  return Number.isNaN(parsed.getTime()) ? String(timestamp) : parsed.toLocaleString(undefined, { dateStyle: "short", timeStyle: "short" });
}

export function formatSize(bytes: number): string {
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  if (bytes < 1024 * 1024) {
    return `${(bytes / 1024).toFixed(1)} KB`;
  }
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
