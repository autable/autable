import {
  Badge,
  Button,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  Link,
  Spinner,
  Table,
  TableBody,
  TableCell,
  TableHeader,
  TableHeaderCell,
  TableRow,
  Text,
  Tooltip
} from "@fluentui/react-components";
import {
  AppsRegular,
  ArrowDownloadRegular,
  ArrowUndoRegular,
  DeleteRegular,
  EyeRegular,
  OpenRegular,
  RocketRegular
} from "@fluentui/react-icons";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { appSourceURL, type AppDefinition, type AppDetail, type AppVersion, type AuthUser } from "../api";

// Fluent tables default to a fixed layout with equal columns; size these to
// their content so version actions and timestamps never get clipped.
const autoLayout = { tableLayout: "auto" } as const;

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
  return (
    <AppListView
      apps={props.apps}
      databaseName={props.databaseName}
      isAdmin={props.isAdmin}
      onSelectApp={props.onSelectApp}
    />
  );
}

function AppLoading() {
  const { t } = useTranslation();
  return (
    <div className="workspace-empty">
      <Spinner size="small" label={t("app.loading")} />
    </div>
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
  if (apps.length === 0) {
    return (
      <div className="workspace-empty">
        <div className="workspace-empty-card app-empty-card">
          <div className="workspace-empty-icon" aria-hidden="true">
            <AppsRegular />
          </div>
          <Text weight="semibold" size={400}>
            {t("app.emptyTitle")}
          </Text>
          <Text size={200} align="center">
            {t("app.emptyDescription")}
          </Text>
          {isAdmin && <AppCommandHelp databaseName={databaseName} />}
        </div>
      </div>
    );
  }
  return (
    <div className="app-workspace">
      <div className="section-header app-section-header">
        <div>
          <Text weight="semibold">{t("app.apps")}</Text>
          <Text size={200}>{t("app.listLabel", { database: databaseName })}</Text>
        </div>
      </div>
      <div className="app-card">
        <Table aria-label={t("app.appTable")} size="small" style={autoLayout}>
          <TableHeader>
            <TableRow>
              <TableHeaderCell>{t("app.title")}</TableHeaderCell>
              <TableHeaderCell>{t("app.name")}</TableHeaderCell>
              <TableHeaderCell>{t("app.publishedVersion")}</TableHeaderCell>
              <TableHeaderCell>{t("app.updated")}</TableHeaderCell>
              <TableHeaderCell className="app-actions-cell">{t("app.actions")}</TableHeaderCell>
            </TableRow>
          </TableHeader>
          <TableBody>
            {apps.map((app) => (
              <TableRow key={app.id}>
                <TableCell>
                  <Link as="button" className="app-title-link" onClick={() => onSelectApp(app.name)}>
                    {app.title || app.name}
                  </Link>
                </TableCell>
                <TableCell>
                  <code>{app.name}</code>
                </TableCell>
                <TableCell>
                  <PublishedBadge version={app.published_version} />
                </TableCell>
                <TableCell className="app-nowrap">{formatTime(app.updated_at)}</TableCell>
                <TableCell className="app-actions-cell">
                  <OpenAppButton app={app} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
      {isAdmin && (
        <div className="app-card app-help-card">
          <Text weight="semibold">{t("app.howToTitle")}</Text>
          <AppCommandHelp databaseName={databaseName} />
        </div>
      )}
    </div>
  );
}

function AppCommandHelp({ databaseName }: { databaseName: string }) {
  const { t } = useTranslation();
  return (
    <div className="app-command-help">
      <Text size={200}>{t("app.stepInit")}</Text>
      <pre className="app-command">{`autablectl app init my-app --database ${databaseName} --name my-app`}</pre>
      <Text size={200}>{t("app.stepPush")}</Text>
      <pre className="app-command">{"cd my-app\nautablectl app push --publish"}</pre>
      <Text size={200}>{t("app.skillHint")}</Text>
      <pre className="app-command">autablectl skill install</pre>
    </div>
  );
}

function PublishedBadge({ version }: { version: number }) {
  const { t } = useTranslation();
  return version > 0 ? (
    <Badge appearance="tint" color="success">
      {t("app.versionLabel", { version })}
    </Badge>
  ) : (
    <Badge appearance="tint" color="informative">
      {t("app.notPublished")}
    </Badge>
  );
}

function OpenAppButton({ app, primary = false }: { app: AppDefinition; primary?: boolean }) {
  const { t } = useTranslation();
  if (app.published_version <= 0) {
    return (
      <Button icon={<OpenRegular />} appearance={primary ? "primary" : "secondary"} size={primary ? "medium" : "small"} disabled>
        {t("app.open")}
      </Button>
    );
  }
  return (
    <Button
      as="a"
      href={app.url}
      target="_blank"
      rel="noopener"
      icon={<OpenRegular />}
      appearance={primary ? "primary" : "secondary"}
      size={primary ? "medium" : "small"}
      aria-label={t("app.openApp", { name: app.title || app.name })}
    >
      {t("app.open")}
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
    <div className="app-workspace">
      <div className="section-header app-section-header">
        <div>
          <Text weight="semibold">{app.title || app.name}</Text>
          <Text size={200}>{t("app.detailLabel", { name: app.name, database: databaseName })}</Text>
        </div>
        <div className="app-header-actions">
          <PublishedBadge version={app.published_version} />
          {isAdmin && (
            <Button icon={<DeleteRegular />} disabled={busy} onClick={() => setPending({ kind: "delete" })}>
              {t("app.deleteApp")}
            </Button>
          )}
          <OpenAppButton app={app} primary />
        </div>
      </div>
      {isAdmin && (
      <div className="app-card app-versions-card">
        <div className="app-card-heading">
          <Text weight="semibold">{t("app.versions")}</Text>
          <Text size={200}>{t("app.serverSDK", { version: sdk.version, min: sdk.min_major })}</Text>
        </div>
        {versions.length === 0 ? (
          <Text size={200}>{t("app.noVersions")}</Text>
        ) : (
          <Table aria-label={t("app.versionTable")} size="small" style={autoLayout}>
            <TableHeader>
              <TableRow>
                <TableHeaderCell className="app-version-cell">{t("app.version")}</TableHeaderCell>
                <TableHeaderCell>{t("app.message")}</TableHeaderCell>
                <TableHeaderCell>{t("app.sdk")}</TableHeaderCell>
                <TableHeaderCell>{t("app.uploadedBy")}</TableHeaderCell>
                <TableHeaderCell>{t("app.uploadedAt")}</TableHeaderCell>
                <TableHeaderCell>{t("app.size")}</TableHeaderCell>
                {isAdmin && <TableHeaderCell className="app-actions-cell">{t("app.actions")}</TableHeaderCell>}
              </TableRow>
            </TableHeader>
            <TableBody>
              {versions.map((version) => {
                const published = version.version === app.published_version;
                const rollback = app.published_version > version.version;
                return (
                  <TableRow key={version.version} aria-label={t("app.versionLabel", { version: version.version })}>
                    <TableCell className="app-version-cell">
                      <span className="app-version-label">
                        <Text weight={published ? "semibold" : "regular"}>{t("app.versionLabel", { version: version.version })}</Text>
                        {published && (
                          <Badge appearance="filled" color="success" size="small">
                            {t("app.published")}
                          </Badge>
                        )}
                      </span>
                    </TableCell>
                    <TableCell className="app-message-cell">
                      {version.message || <Text className="app-muted">{t("app.noMessage")}</Text>}
                    </TableCell>
                    <TableCell className="app-nowrap">
                      <span className="app-version-label">
                        <code>{version.sdk}</code>
                        {!version.supported && (
                          <Tooltip content={version.sdk_error ?? t("app.unsupported")} relationship="description">
                            <Badge appearance="tint" color="danger" size="small" tabIndex={0}>
                              {t("app.unsupported")}
                            </Badge>
                          </Tooltip>
                        )}
                      </span>
                    </TableCell>
                    <TableCell>
                      <span className="app-uploader" title={version.created_by}>
                        {version.created_by === currentUser?.id ? currentUser.display_name : version.created_by}
                      </span>
                    </TableCell>
                    <TableCell className="app-nowrap">{formatTime(version.created_at)}</TableCell>
                    <TableCell className="app-nowrap">{formatSize(version.size)}</TableCell>
                    {isAdmin && (
                      <TableCell className="app-actions-cell">
                        <span className="app-row-actions">
                          <Tooltip content={t("app.previewVersion", { version: version.version })} relationship="label">
                            <Button
                              as="a"
                              href={version.preview_url}
                              target="_blank"
                              rel="noopener"
                              size="small"
                              appearance="subtle"
                              icon={<EyeRegular />}
                            />
                          </Tooltip>
                          <Tooltip content={t("app.downloadSourceVersion", { version: version.version })} relationship="label">
                            <Button
                              as="a"
                              href={appSourceURL(databaseName, app.name, version.version)}
                              download
                              size="small"
                              appearance="subtle"
                              icon={<ArrowDownloadRegular />}
                            />
                          </Tooltip>
                          {!published && (
                            <Button
                              size="small"
                              appearance={rollback ? "secondary" : "primary"}
                              icon={rollback ? <ArrowUndoRegular /> : <RocketRegular />}
                              disabled={busy || !version.supported}
                              aria-label={t(rollback ? "app.rollbackVersion" : "app.publishVersion", { version: version.version })}
                              onClick={() => requestPublish(version)}
                            >
                              {t(rollback ? "app.rollback" : "app.publish")}
                            </Button>
                          )}
                        </span>
                      </TableCell>
                    )}
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        )}
      </div>
      )}
      <Dialog open={pending !== null} onOpenChange={(_, data) => !data.open && setPending(null)}>
        <DialogSurface aria-label={pending?.kind === "delete" ? t("app.deleteTitle") : t("app.rollbackTitle")}>
          <DialogBody>
            <DialogTitle>{pending?.kind === "delete" ? t("app.deleteTitle") : t("app.rollbackTitle")}</DialogTitle>
            <DialogContent>
              {pending?.kind === "delete"
                ? t("app.deleteConfirm", { name: app.title || app.name })
                : t("app.rollbackConfirm", {
                    current: app.published_version,
                    version: pending?.kind === "rollback" ? pending.version : 0
                  })}
            </DialogContent>
            <DialogActions>
              <Button onClick={() => setPending(null)}>{t("common.cancel")}</Button>
              <Button appearance="primary" onClick={confirmPending}>
                {pending?.kind === "delete" ? t("common.delete") : t("app.rollback")}
              </Button>
            </DialogActions>
          </DialogBody>
        </DialogSurface>
      </Dialog>
    </div>
  );
}

function formatTime(timestamp: number): string {
  const parsed = new Date(timestamp);
  return Number.isNaN(parsed.getTime())
    ? String(timestamp)
    : parsed.toLocaleString(undefined, { dateStyle: "short", timeStyle: "short" });
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
