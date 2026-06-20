import DataGrid, { type DataGridProps } from "react-data-grid";
import type { TableGridRow } from "../tableGrid";

type RecordDataGridProps<R extends TableGridRow> = Omit<DataGridProps<R>, "className" | "defaultColumnOptions"> & {
  className?: string;
  defaultColumnOptions?: DataGridProps<R>["defaultColumnOptions"];
};

export function RecordDataGrid<R extends TableGridRow>({
  className,
  defaultColumnOptions,
  ...props
}: RecordDataGridProps<R>) {
  return (
    <DataGrid
      {...props}
      className={["autable-grid", "rdg-light", className].filter(Boolean).join(" ")}
      defaultColumnOptions={{ resizable: true, ...defaultColumnOptions }}
    />
  );
}
