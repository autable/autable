import { Text } from "@fluentui/react-components";
import type { FormEvent } from "react";
import type { TableMetadata } from "../api";
import type { FormRenderResult } from "../formRuntime";
import { FormPreviewFields } from "./FormPreviewFields";

type FormRunnerProps = {
  className?: string;
  databaseName: string;
  renderedForm: FormRenderResult;
  result?: unknown;
  tables: TableMetadata[];
  title?: string;
  values: Record<string, string>;
  onAction: (actionID: string, valueOverrides?: Record<string, string>) => void | Promise<void>;
  onSubmit: (event?: FormEvent<HTMLFormElement>) => void | Promise<void>;
  onValueChange: (name: string, value: string) => void;
};

export function FormRunner({
  className = "form-preview",
  databaseName,
  renderedForm,
  result,
  tables,
  title,
  values,
  onAction,
  onSubmit,
  onValueChange
}: FormRunnerProps) {
  return (
    <form className={className} onSubmit={(event) => onSubmit(event)}>
      {title && <Text weight="semibold">{title}</Text>}
      {renderedForm.error && <Text className="form-error">{renderedForm.error}</Text>}
      <FormPreviewFields
        databaseName={databaseName}
        elements={renderedForm.elements}
        formValues={values}
        result={result}
        onAction={onAction}
        onFormValueChange={onValueChange}
        tables={tables}
      />
    </form>
  );
}
