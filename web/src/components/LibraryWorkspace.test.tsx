import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { LibraryDefinition, LibraryDetail, WorkflowRun } from "../api";
import i18n from "../i18n";
import { NavigationProvider } from "../shell/navigation";
import { TooltipProvider } from "../ui";
import { LibraryWorkspace } from "./LibraryWorkspace";
import { WorkflowRunDetail } from "./WorkflowRunDetail";

const http: LibraryDefinition = {
  id: 1,
  name: "http",
  title: "HTTP helpers",
  published_version: 2,
  latest_version: 3,
  created_at: 1781600000000,
  updated_at: 1781600000000,
  imported_by: [
    { kind: "workflow", database_name: "ops", name: "order-sync", title: "Order sync", version: 4 },
    { kind: "library", name: "notify", title: "Notify", version: 1 }
  ]
};

const unused: LibraryDefinition = { ...http, id: 2, name: "format", title: "", published_version: 0, imported_by: [] };

const detail: LibraryDetail = {
  library: http,
  versions: [1, 3, 2].map((version) => ({
    library_id: 1,
    version,
    sdk: "1.0",
    message: `change ${version}`,
    size: 2048 * version,
    imports: version === 2 ? ["text"] : [],
    created_by: "u1",
    created_at: 1781600000000 + version,
    published: version === 2,
    supported: true
  })),
  sdk: { version: "1.0", min_major: 1 }
};

function renderWorkspace(props: Partial<Parameters<typeof LibraryWorkspace>[0]> = {}) {
  const handlers = { onDelete: vi.fn(), onPublish: vi.fn() };
  const navigate = vi.fn();
  render(
    <NavigationProvider navigate={navigate}>
      <TooltipProvider>
        <LibraryWorkspace
          busy={false}
          currentUser={null}
          databaseName="ops"
          detail={null}
          libraries={[http, unused]}
          librariesReady
          libraryName=""
          {...handlers}
          {...props}
        />
      </TooltipProvider>
    </NavigationProvider>
  );
  return { ...handlers, navigate };
}

beforeEach(async () => {
  await i18n.changeLanguage("en-US");
});

describe("LibraryWorkspace", () => {
  it("lists libraries with their published version and importers", async () => {
    const { navigate } = renderWorkspace();
    const table = screen.getByRole("table", { name: "Libraries" });
    const rows = within(table).getAllByRole("row");
    expect(rows).toHaveLength(3);
    expect(within(rows[1]).getByText("HTTP helpers")).toBeInTheDocument();
    expect(within(rows[1]).getByText("@lib/http")).toBeInTheDocument();
    expect(within(rows[1]).getByText("1 workflow · 1 library")).toBeInTheDocument();
    expect(within(rows[2]).getByText("format")).toBeInTheDocument();
    expect(within(rows[2]).getByText("Not imported")).toBeInTheDocument();
    expect(within(rows[2]).getByText("Not published")).toBeInTheDocument();
    await userEvent.click(within(rows[1]).getByRole("link"));
    expect(navigate).toHaveBeenCalledWith({ databaseName: "ops", view: "library", libraryName: "http" });
  });

  it("explains how to create a library when there is none", () => {
    renderWorkspace({ libraries: [] });
    expect(screen.getByText("No libraries yet")).toBeInTheDocument();
    expect(screen.getByText("autablectl lib init my-lib --name my-lib")).toBeInTheDocument();
  });

  it("shows versions newest first, importers, and confirms publish and rollback", async () => {
    const { onPublish, onDelete } = renderWorkspace({ libraryName: "http", detail });
    expect(screen.getByRole("heading", { name: "HTTP helpers" })).toBeInTheDocument();
    expect(screen.getByText(/changes every importing workflow at its next run/)).toBeInTheDocument();

    const importers = screen.getByRole("table", { name: "Imported by" });
    expect(within(importers).getByRole("link", { name: "Order sync" })).toHaveAttribute("href", "/databases/ops/workflows/order-sync");
    expect(within(importers).getByText("Workflow in ops")).toBeInTheDocument();
    expect(within(importers).getByRole("link", { name: "Notify" })).toHaveAttribute("href", "/databases/ops/libraries/notify");

    const versions = within(screen.getByRole("table", { name: "Library versions" })).getAllByRole("row").slice(1);
    expect(versions.map((row) => row.getAttribute("aria-label"))).toEqual(["v3", "v2", "v1"]);
    expect(within(versions[1]).getByText("Published")).toBeInTheDocument();
    expect(within(versions[1]).getByText("@lib/text")).toBeInTheDocument();
    expect(within(versions[2]).getByRole("link", { name: "Download source of version 1" })).toHaveAttribute("href", "/api/libs/http/versions/1/source");

    await userEvent.click(within(versions[0]).getByRole("button", { name: "Publish version 3" }));
    const publishDialog = await screen.findByRole("dialog");
    expect(within(publishDialog).getByText(/1 importing workflow uses it from its next run/)).toBeInTheDocument();
    await userEvent.click(within(publishDialog).getByRole("button", { name: "Publish" }));
    expect(onPublish).toHaveBeenCalledWith(3);

    await userEvent.click(within(versions[2]).getByRole("button", { name: "Roll back to version 1" }));
    const rollbackDialog = await screen.findByRole("dialog");
    expect(within(rollbackDialog).getByText(/from version 2 to 1/)).toBeInTheDocument();
    await userEvent.click(within(rollbackDialog).getByRole("button", { name: "Roll back" }));
    expect(onPublish).toHaveBeenCalledWith(1);

    // An imported library cannot be deleted.
    expect(screen.getByRole("button", { name: "Delete library" })).toBeDisabled();
    expect(onDelete).not.toHaveBeenCalled();
  });

  it("deletes a library nothing imports after confirmation", async () => {
    const { onDelete } = renderWorkspace({ libraryName: "format", detail: { ...detail, library: unused } });
    await userEvent.click(screen.getByRole("button", { name: "Delete library" }));
    await userEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Delete" }));
    expect(onDelete).toHaveBeenCalled();
  });
});

describe("WorkflowRunDetail libraries", () => {
  it("shows the library versions a run loaded", () => {
    const run: WorkflowRun = {
      id: 9,
      workflow_id: 4,
      version: 2,
      trigger: "start",
      status: "succeeded",
      created_at: 1781600000000,
      libraries: [
        { name: "text", version: 1 },
        { name: "http", version: 3 }
      ]
    };
    render(<WorkflowRunDetail run={run} />);
    const libraries = screen.getByLabelText("Libraries");
    expect(within(libraries).getByText("@lib/text v1")).toBeInTheDocument();
    expect(within(libraries).getByText("@lib/http v3")).toBeInTheDocument();
  });
});
