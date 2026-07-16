import { useState } from "react";
import {
  Button,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  DialogTrigger,
  Text,
  ToolbarButton,
  Tooltip
} from "@fluentui/react-components";
import { ArrowSyncCircleRegular, KeyResetRegular } from "@fluentui/react-icons";
import { useTranslation } from "react-i18next";
import { fetchRunners, resetRunnerToken, type RunnersResponse } from "../api";

export function RunnersPanel({ disabled }: { disabled: boolean }) {
  const { t, i18n } = useTranslation();
  const [open, setOpen] = useState(false);
  const [runnersInfo, setRunnersInfo] = useState<RunnersResponse | null>(null);
  const [freshToken, setFreshToken] = useState("");
  const [error, setError] = useState("");

  async function refresh() {
    try {
      setRunnersInfo(await fetchRunners());
      setError("");
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : t("runners.loadFailed"));
    }
  }

  async function reset() {
    try {
      const result = await resetRunnerToken();
      setFreshToken(result.token);
      await refresh();
    } catch (resetError) {
      setError(resetError instanceof Error ? resetError.message : t("runners.resetFailed"));
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(_, data) => {
        setOpen(data.open);
        if (data.open) {
          setFreshToken("");
          void refresh();
        }
      }}
    >
      <DialogTrigger disableButtonEnhancement>
        <Tooltip content={t("runners.title")} relationship="label">
          <ToolbarButton aria-label={t("runners.title")} icon={<ArrowSyncCircleRegular />} disabled={disabled} />
        </Tooltip>
      </DialogTrigger>
      <DialogSurface aria-label={t("runners.title")}>
        <DialogBody>
          <DialogTitle>{t("runners.title")}</DialogTitle>
          <DialogContent>
            <div className="runners-panel">
              {error !== "" && <Text role="alert">{error}</Text>}
              <Text weight="semibold">{t("runners.connected")}</Text>
              {(runnersInfo?.runners ?? []).length === 0 ? (
                <Text size={200}>{t("runners.empty")}</Text>
              ) : (
                <ul className="runners-list" aria-label={t("runners.connected")}>
                  {(runnersInfo?.runners ?? []).map((runner) => (
                    <li key={`${runner.name}-${runner.connected_at}`}>
                      <Text weight="semibold">{runner.name}</Text>
                      <Text size={200}>
                        {t("runners.details", {
                          version: runner.version,
                          nodes: runner.node_types.length,
                          connectedAt: new Date(runner.connected_at).toLocaleString(i18n.language)
                        })}
                      </Text>
                    </li>
                  ))}
                </ul>
              )}
              <Text weight="semibold">{t("runners.token")}</Text>
              <Text size={200}>
                {runnersInfo?.token.exists
                  ? t("runners.tokenCreatedAt", {
                      createdAt: new Date(runnersInfo.token.created_at ?? 0).toLocaleString(i18n.language)
                    })
                  : t("runners.tokenMissing")}
              </Text>
              {freshToken !== "" && (
                <div className="runners-fresh-token">
                  <Text size={200}>{t("runners.tokenShownOnce")}</Text>
                  <code aria-label={t("runners.tokenValue")}>{freshToken}</code>
                </div>
              )}
            </div>
          </DialogContent>
          <DialogActions>
            <Button appearance="primary" icon={<KeyResetRegular />} onClick={() => void reset()}>
              {runnersInfo?.token.exists ? t("runners.reset") : t("runners.generate")}
            </Button>
            <DialogTrigger disableButtonEnhancement>
              <Button appearance="secondary">{t("common.close")}</Button>
            </DialogTrigger>
          </DialogActions>
        </DialogBody>
      </DialogSurface>
    </Dialog>
  );
}
