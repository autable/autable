import { DataGrid, type DataGridProps } from "react-data-grid";
import type { TableGridRow } from "../tableGrid";

type RecordDataGridProps<R extends TableGridRow> = Omit<DataGridProps<R>, "className" | "defaultColumnOptions"> & {
  className?: string;
  defaultColumnOptions?: DataGridProps<R>["defaultColumnOptions"];
};

// The grid takes its colors from the design tokens (see .autable-grid in
// app.css), so it follows the light and dark themes.
export function RecordDataGrid<R extends TableGridRow>({
  className,
  defaultColumnOptions,
  rowHeight = 36,
  headerRowHeight = 38,
  ...props
}: RecordDataGridProps<R>) {
  return (
    <DataGrid
      {...props}
      rowHeight={rowHeight}
      headerRowHeight={headerRowHeight}
      className={["autable-grid", className].filter(Boolean).join(" ")}
      defaultColumnOptions={{ resizable: true, ...defaultColumnOptions }}
    />
  );
}
