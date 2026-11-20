import { ArrowLeft, Download, Library, Rocket, Trash2, Undo2, Workflow } from "lucide-react";
import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { librarySourceURL, type AuthUser, type LibraryDefinition, type LibraryDetail, type LibraryImporter, type LibraryVersion } from "../api";
import { Page, SectionHeading } from "../shell/Page";
import { RouteLink } from "../shell/navigation";
import {
  Badge,
  Button,
  Callout,
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
import { formatSize, PublishedStatus } from "./AppWorkspace";

type LibraryWorkspaceProps = {
  busy: boolean;
  currentUser: AuthUser | null;
  // The database whose workspace shows the page; libraries themselves are
  // instance-wide.
  databaseName: string;
  detail: LibraryDetail | null;
  libraries: LibraryDefinition[];
  librariesReady: boolean;
  libraryName: string;
  onDelete: () => void;
  onPublish: (version: number) => void;
};

/** The admin pages for libraries: the list, and one library's detail. */
export function LibraryWorkspace(props: LibraryWorkspaceProps) {
  if (props.libraryName) {
    return props.detail ? (
      <LibraryDetailView
        busy={props.busy}
        currentUser={props.currentUser}
        databaseName={props.databaseName}
        detail={props.detail}
        onDelete={props.onDelete}
        onPublish={props.onPublish}
      />
    ) : (
      <LibraryLoading />
    );
  }
  if (!props.librariesReady) {
    return <LibraryLoading />;
  }
  return <LibraryListView databaseName={props.databaseName} libraries={props.libraries} />;
}

function LibraryLoading() {
  const { t } = useTranslation();
  return (
    <Page title={t("library.title")}>
      <div role="status" aria-label={t("library.loading")} className="flex flex-col gap-3">
        <Skeleton className="h-11" />
        <Skeleton className="h-11" />
        <Skeleton className="h-11" />
      </div>
    </Page>
  );
}

function LibraryListView({ databaseName, libraries }: { databaseName: string; libraries: LibraryDefinition[] }) {
  const { t } = useTranslation();
  return (
    <Page title={t("library.title")} description={t("library.description")}>
      {libraries.length === 0 ? (
        <Card>
          <EmptyState icon={Library} title={t("library.emptyTitle")} description={t("library.emptyDescription")} action={<LibraryCommandHelp />} />
        </Card>
      ) : (
        <>
          <Card className="overflow-hidden">
            <Table aria-label={t("library.table")}>
              <THead>
                <tr>
                  <Th>{t("library.name")}</Th>
                  <Th>{t("library.publishedVersion")}</Th>
                  <Th className="hidden md:table-cell">{t("library.importedBy")}</Th>
                  <Th className="hidden lg:table-cell">{t("library.updated")}</Th>
                </tr>
              </THead>
              <TBody>
                {libraries.map((library) => (
                  <Tr key={library.id} className="relative">
                    <Td className="py-2">
                      <RouteLink
                        route={{ databaseName, view: "library", libraryName: library.name }}
                        className="flex min-w-0 items-center gap-3 after:absolute after:inset-0"
                      >
                        <LibraryTile library={library} />
                        <span className="flex min-w-0 flex-col">
                          <span className="truncate text-14 font-medium text-fg">{library.title || library.name}</span>
                          <code className="truncate font-mono text-12 text-muted">@lib/{library.name}</code>
                        </span>
                      </RouteLink>
                    </Td>
                    <Td>
                      <PublishedStatus version={library.published_version} />
                    </Td>
                    <Td className="hidden text-description md:table-cell">{importerSummary(t, library.imported_by)}</Td>
                    <Td className="hidden whitespace-nowrap text-muted lg:table-cell">{formatTime(library.updated_at)}</Td>
                  </Tr>
                ))}
              </TBody>
            </Table>
          </Card>
          <div className="mt-8">
            <SectionHeading title={t("library.howToTitle")} />
            <Card className="px-5 py-4">
              <LibraryCommandHelp />
            </Card>
          </div>
        </>
      )}
    </Page>
  );
}

function importerSummary(t: (key: string, options?: Record<string, unknown>) => string, importers: LibraryImporter[]): string {
  if (importers.length === 0) {
    return t("library.noImporters");
  }
  const workflows = importers.filter((item) => item.kind === "workflow").length;
  const libraries = importers.length - workflows;
  const parts: string[] = [];
  if (workflows > 0) parts.push(t("library.workflowCount", { count: workflows }));
  if (libraries > 0) parts.push(t("library.libraryCount", { count: libraries }));
  return parts.join(" · ");
}

function LibraryCommandHelp() {
  const { t } = useTranslation();
  return (
    <div className="flex w-full max-w-xl flex-col gap-2 text-left">
      <CommandStep label={t("library.stepInit")} command="autablectl lib init my-lib --name my-lib" />
      <CommandStep label={t("library.stepPush")} command={"cd my-lib\nautablectl lib push -m \"first version\"\nautablectl lib publish my-lib 1"} />
      <CommandStep label={t("library.stepImport")} command={'import { postJSON } from "@lib/my-lib";\nautablectl workflow sdk'} />
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

function LibraryTile({ library, size = "md" }: { library: LibraryDefinition; size?: "md" | "lg" }) {
  return (
    <span
      aria-hidden
      className={cn("flex shrink-0 items-center justify-center rounded-inner bg-chip text-description", size === "lg" ? "size-10" : "size-8")}
      title={library.name}
    >
      <Library className={size === "lg" ? "size-5" : "size-4"} strokeWidth={1.75} />
    </span>
  );
}

type PendingAction = { kind: "rollback"; version: number } | { kind: "publish"; version: number } | { kind: "delete" } | null;

function LibraryDetailView({
  busy,
  currentUser,
  databaseName,
  detail,
  onDelete,
  onPublish
}: {
  busy: boolean;
  currentUser: AuthUser | null;
  databaseName: string;
  detail: LibraryDetail;
  onDelete: () => void;
  onPublish: (version: number) => void;
}) {
  const { t } = useTranslation();
  const [pending, setPending] = useState<PendingAction>(null);
  const { library, sdk } = detail;
  const versions = [...detail.versions].sort((left, right) => right.version - left.version);
  const published = versions.find((version) => version.version === library.published_version);
  const importers = library.imported_by;

  function confirmPending() {
    if (pending?.kind === "rollback" || pending?.kind === "publish") {
      onPublish(pending.version);
    } else if (pending?.kind === "delete") {
      onDelete();
    }
    setPending(null);
  }

  const dialog = dialogText(t, pending, library);
  return (
    <Page
      width="wide"
      title={library.title || library.name}
      description={t("library.detailLabel", { name: library.name })}
      icon={<LibraryTile library={library} size="lg" />}
      back={
        <Button asChild variant="quiet" size="sm" className="-ml-2.5">
          <RouteLink route={{ databaseName, view: "library" }}>
            <ArrowLeft aria-hidden />
            {t("library.back")}
          </RouteLink>
        </Button>
      }
      actions={
        <Tooltip content={importers.length > 0 ? t("library.deleteBlocked") : t("library.delete")}>
          <span>
            <Button variant="danger-quiet" disabled={busy || importers.length > 0} onClick={() => setPending({ kind: "delete" })}>
              <Trash2 aria-hidden />
              {t("library.delete")}
            </Button>
          </span>
        </Tooltip>
      }
    >
      <section aria-labelledby="library-about-heading" className="mb-8">
        <h2 id="library-about-heading" className="sr-only">
          {t("library.about")}
        </h2>
        <Card>
          <dl className="grid grid-cols-2 gap-x-6 gap-y-4 px-5 py-4 text-13 md:grid-cols-4">
            <AboutItem label={t("library.publishedVersion")}>
              <PublishedStatus version={library.published_version} />
            </AboutItem>
            <AboutItem label={t("library.import")}>
              <code className="font-mono text-12 text-description">@lib/{library.name}</code>
            </AboutItem>
            <AboutItem label={t("library.imports")}>
              {published && published.imports.length > 0 ? <LibraryNames databaseName={databaseName} names={published.imports} /> : t("library.none")}
            </AboutItem>
            <AboutItem label={t("library.updated")}>{formatTime(library.updated_at)}</AboutItem>
          </dl>
        </Card>
        <Callout tone="warning" className="mt-3">
          {t("library.publishWarning")}
        </Callout>
      </section>

      <section aria-labelledby="library-importers-heading" className="mb-8 min-w-0">
        <SectionHeading id="library-importers-heading" title={t("library.importedBy")} />
        <Card className="overflow-hidden">
          {importers.length === 0 ? (
            <p className="px-5 py-6 text-center text-13 text-muted">{t("library.noImportersLong")}</p>
          ) : (
            <Table aria-label={t("library.importedBy")}>
              <THead>
                <tr>
                  <Th>{t("library.importer")}</Th>
                  <Th className="hidden sm:table-cell">{t("library.kind")}</Th>
                  <Th>{t("library.importerVersion")}</Th>
                </tr>
              </THead>
              <TBody>
                {importers.map((importer) => (
                  <Tr key={`${importer.kind}:${importer.database_name ?? ""}:${importer.name}`}>
                    <Td>
                      <ImporterLink databaseName={databaseName} importer={importer} />
                    </Td>
                    <Td className="hidden text-description sm:table-cell">
                      {importer.kind === "workflow" ? t("library.kindWorkflow", { database: importer.database_name }) : t("library.kindLibrary")}
                    </Td>
                    <Td className="tabular-nums text-description">{t("app.versionLabel", { version: importer.version })}</Td>
                  </Tr>
                ))}
              </TBody>
            </Table>
          )}
        </Card>
      </section>

      <section aria-labelledby="library-versions-heading" className="min-w-0">
        <SectionHeading
          id="library-versions-heading"
          title={t("library.versions")}
          action={<span className="text-12 text-subtle">{t("app.serverSDK", { version: sdk.version, min: sdk.min_major })}</span>}
        />
        <Card className="overflow-hidden">
          {versions.length === 0 ? (
            <p className="px-5 py-8 text-center text-13 text-muted">{t("app.noVersions")}</p>
          ) : (
            <Table aria-label={t("library.versionTable")}>
              <THead>
                <tr>
                  <Th>{t("app.version")}</Th>
                  <Th>{t("app.message")}</Th>
                  <Th className="hidden lg:table-cell">{t("library.imports")}</Th>
                  <Th className="hidden lg:table-cell">{t("app.sdk")}</Th>
                  <Th className="hidden xl:table-cell">{t("app.uploadedBy")}</Th>
                  <Th className="hidden md:table-cell">{t("app.uploadedAt")}</Th>
                  <Th className="hidden xl:table-cell">{t("app.size")}</Th>
                  <Th className="text-right">
                    <span className="sr-only">{t("app.actions")}</span>
                  </Th>
                </tr>
              </THead>
              <TBody>
                {versions.map((version) => (
                  <VersionRow
                    key={version.version}
                    busy={busy}
                    currentUser={currentUser}
                    library={library}
                    onPublish={(target) =>
                      setPending(library.published_version > target.version ? { kind: "rollback", version: target.version } : { kind: "publish", version: target.version })
                    }
                    version={version}
                  />
                ))}
              </TBody>
            </Table>
          )}
        </Card>
      </section>

      <Dialog open={pending !== null} onOpenChange={(open) => !open && setPending(null)}>
        <DialogContent
          title={dialog.title}
          description={dialog.description}
          footer={
            <>
              <DialogClose asChild>
                <Button>{t("common.cancel")}</Button>
              </DialogClose>
              <Button variant={pending?.kind === "delete" ? "danger" : "cta"} onClick={confirmPending}>
                {dialog.confirm}
              </Button>
            </>
          }
        />
      </Dialog>
    </Page>
  );
}

function dialogText(
  t: (key: string, options?: Record<string, unknown>) => string,
  pending: PendingAction,
  library: LibraryDefinition
): { title: string; description: string; confirm: string } {
  const name = library.title || library.name;
  const workflows = library.imported_by.filter((item) => item.kind === "workflow").length;
  switch (pending?.kind) {
    case "delete":
      return { title: t("library.deleteTitle"), description: t("library.deleteConfirm", { name }), confirm: t("common.delete") };
    case "rollback":
      return {
        title: t("library.rollbackTitle"),
        description: t("library.rollbackConfirm", { name, current: library.published_version, version: pending.version, count: workflows }),
        confirm: t("app.rollback")
      };
    case "publish":
      return {
        title: t("library.publishTitle"),
        description: t("library.publishConfirm", { name, version: pending.version, count: workflows }),
        confirm: t("app.publish")
      };
    default:
      return { title: "", description: "", confirm: "" };
  }
}

function ImporterLink({ databaseName, importer }: { databaseName: string; importer: LibraryImporter }) {
  const label = importer.title || importer.name;
  if (importer.kind === "workflow" && importer.database_name) {
    return (
      <RouteLink
        route={{ databaseName: importer.database_name, view: "workflow", workflowName: importer.name }}
        className="inline-flex min-w-0 items-center gap-2 font-medium text-fg underline-offset-4 hover:underline"
      >
        <Workflow className="size-4 shrink-0 text-muted" strokeWidth={1.75} aria-hidden />
        <span className="truncate">{label}</span>
      </RouteLink>
    );
  }
  return (
    <RouteLink
      route={{ databaseName, view: "library", libraryName: importer.name }}
      className="inline-flex min-w-0 items-center gap-2 font-medium text-fg underline-offset-4 hover:underline"
    >
      <Library className="size-4 shrink-0 text-muted" strokeWidth={1.75} aria-hidden />
      <span className="truncate">{label}</span>
    </RouteLink>
  );
}

function LibraryNames({ databaseName, names }: { databaseName: string; names: string[] }) {
  return (
    <span className="flex flex-wrap gap-1">
      {names.map((name) => (
        <RouteLink key={name} route={{ databaseName, view: "library", libraryName: name }}>
          <Badge className="font-mono">@lib/{name}</Badge>
        </RouteLink>
      ))}
    </span>
  );
}

function AboutItem({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <dt className="text-12 text-muted">{label}</dt>
      <dd className="min-w-0 text-fg">{children}</dd>
    </div>
  );
}

function VersionRow({
  busy,
  currentUser,
  library,
  onPublish,
  version
}: {
  busy: boolean;
  currentUser: AuthUser | null;
  library: LibraryDefinition;
  onPublish: (version: LibraryVersion) => void;
  version: LibraryVersion;
}) {
  const { t } = useTranslation();
  const published = version.version === library.published_version;
  const rollback = library.published_version > version.version;
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
        {version.message ? <span className="line-clamp-2">{version.message}</span> : <span className="text-subtle">{t("app.noMessage")}</span>}
      </Td>
      <Td className="hidden lg:table-cell">
        {version.imports.length > 0 ? (
          <span className="font-mono text-12 text-description">{version.imports.map((name) => `@lib/${name}`).join(", ")}</span>
        ) : (
          <span className="text-subtle">{published ? t("library.none") : "—"}</span>
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
      <Td className="hidden max-w-40 xl:table-cell">
        <span className="block truncate text-description" title={version.created_by}>
          {version.created_by === currentUser?.id ? currentUser.display_name : version.created_by}
        </span>
      </Td>
      <Td className="hidden whitespace-nowrap text-muted md:table-cell">{formatTime(version.created_at)}</Td>
      <Td className="hidden whitespace-nowrap tabular-nums text-muted xl:table-cell">{formatSize(version.size)}</Td>
      <Td className="whitespace-nowrap text-right">
        <span className="inline-flex items-center justify-end gap-0.5">
          <Tooltip content={source}>
            <Button asChild variant="quiet" size="icon-sm">
              <a href={librarySourceURL(library.name, version.version)} download aria-label={source}>
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
