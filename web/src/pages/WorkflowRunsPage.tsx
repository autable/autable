import { Play, Workflow } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { WorkflowDefinition } from "../api";
import { Page } from "../shell/Page";
import { Button, Card, EmptyState, Table, TBody, Td, Th, THead, Tr } from "../ui";

/**
 * Workflows for people who may run but not edit them: the granted
 * workflows, each with a Run button. Definitions and history stay with admins.
 */
export function WorkflowRunsPage({
  databaseName,
  onRun,
  workflows
}: {
  databaseName: string;
  onRun: (workflow: WorkflowDefinition) => void | Promise<unknown>;
  workflows: WorkflowDefinition[];
}) {
  const { t } = useTranslation();
  const [running, setRunning] = useState<Record<number, boolean>>({});

  async function run(workflow: WorkflowDefinition) {
    const id = workflow.id ?? 0;
    setRunning((current) => ({ ...current, [id]: true }));
    try {
      await onRun(workflow);
    } finally {
      setRunning((current) => ({ ...current, [id]: false }));
    }
  }

  return (
    <Page title={t("nav.workflows")} description={t("workflow.runnableDescription", { database: databaseName })}>
      <Card className="overflow-hidden">
        {workflows.length === 0 ? (
          <EmptyState icon={Workflow} title={t("empty.noWorkflowTitle")} description={t("empty.noRunnableWorkflowDescription")} />
        ) : (
          <Table aria-label={t("workflow.runnableList")}>
            <THead>
              <tr>
                <Th>{t("nav.name")}</Th>
                <Th className="text-right">
                  <span className="sr-only">{t("app.actions")}</span>
                </Th>
              </tr>
            </THead>
            <TBody>
              {workflows.map((workflow) => (
                <Tr key={workflow.id ?? workflow.name}>
                  <Td>
                    <span className="flex items-center gap-3">
                      <span className="flex size-8 shrink-0 items-center justify-center rounded-inner bg-chip text-description" aria-hidden>
                        <Workflow className="size-4" strokeWidth={1.75} />
                      </span>
                      <span className="font-medium">{workflow.name}</span>
                    </span>
                  </Td>
                  <Td className="text-right">
                    <Button
                      size="sm"
                      aria-label={t("workflow.runWorkflow", { name: workflow.name })}
                      loading={Boolean(workflow.id && running[workflow.id])}
                      disabled={!workflow.id}
                      onClick={() => void run(workflow)}
                    >
                      <Play aria-hidden />
                      {t("common.run")}
                    </Button>
                  </Td>
                </Tr>
              ))}
            </TBody>
          </Table>
        )}
      </Card>
    </Page>
  );
}
