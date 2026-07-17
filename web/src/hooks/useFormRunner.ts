import type { Notify } from "../notifications";
import { useEffect, useMemo, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import {
  createRow,
  listRows,
  updateRow,
  upsertRow,
  type RowListOptions
} from "../api";
import { renderFormScript, type FormActionAPI } from "../formRuntime";

type UseFormRunnerOptions = {
  databaseName: string;
  script: string;
  onStatus: Notify;
  onRowCreated?: (tableName: string, row: Awaited<ReturnType<typeof createRow>>) => void;
};

export function useFormRunner({ databaseName, script, onStatus, onRowCreated }: UseFormRunnerOptions) {
  const { t } = useTranslation();
  const [values, setValues] = useState<Record<string, string>>({});
  const [result, setResult] = useState<unknown>(undefined);
  const rendered = useMemo(() => renderFormScript(script), [script]);

  useEffect(() => {
    setValues({});
    setResult(undefined);
  }, [script]);

  function updateValue(field: string, value: string) {
    setValues((current) => ({ ...current, [field]: value }));
  }

  async function submit(event?: FormEvent<HTMLFormElement>) {
    event?.preventDefault();
    await execute("submit");
  }

  async function execute(actionID: string, valueOverrides: Record<string, string> = {}) {
    const action = rendered.actions[actionID];
    if (!action) {
      onStatus(t("status.formRenderTargetRequired"));
      return;
    }
    try {
      const actionResult = await action(actionAPI(valueOverrides));
      if (actionResult !== undefined) {
        setResult(actionResult);
      }
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.formSubmitFailed"), "error");
    }
  }

  function actionAPI(valueOverrides: Record<string, string>): FormActionAPI {
    const mergedValues = () => valuesWithDefaults(valueOverrides);
    return {
      value: (field) => mergedValues()[field] ?? "",
      values: mergedValues,
      setValue: updateValue,
      rows: {
        create: async (table, rowValues) => {
          const saved = await createRow(databaseName, table, rowValues);
          onRowCreated?.(table, saved);
          return saved;
        },
        update: async (table, recordID, rowValues) => updateRow(databaseName, table, recordID, rowValues),
        upsert: async (table, input) => upsertRow(databaseName, table, input.match_field, input.values),
        list: async (table, options) => listRows(databaseName, table, undefined, undefined, options as RowListOptions)
      },
      show: setResult
    };
  }

  function valuesWithDefaults(overrides: Record<string, string> = {}): Record<string, string> {
    return Object.fromEntries(
      rendered.elements.flatMap((element) => {
        if (element.kind === "input" || element.kind === "relation" || element.kind === "file") {
          return [[element.field, overrides[element.field] ?? values[element.field] ?? ""]];
        }
        if (element.kind === "select") {
          return [[element.field, overrides[element.field] ?? values[element.field] ?? element.options[0] ?? ""]];
        }
        return [];
      })
    );
  }

  return {
    execute,
    rendered,
    result,
    submit,
    updateValue,
    values
  };
}
