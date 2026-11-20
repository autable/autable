import { isPermissionError, log, manual, schedule, tables, TABLE_NAMES, type TriggerEvent } from "./autable";

// Triggers are read when a version is published. Each name becomes
// event.trigger in run().
export const triggers = {
  // Started by hand: autablectl workflow run <database> <name> --input '{"note":"hi"}' --wait
  start: manual({ fields: [{ name: "note", label: "Note", type: "string" }] }),
  // Every day at 06:00 UTC.
  daily: schedule({ cron: "0 6 * * *", timezone: "UTC" }),
};

// One run = one call of run(), from start to finish. What it returns is
// stored as the run's output.
export async function run(event: TriggerEvent<typeof triggers>) {
  switch (event.trigger) {
    case "start": {
      log.info("started by hand", { note: event.input.note ?? null, by: event.startedBy?.email ?? null });
      return { rows: await countRows() };
    }
    case "daily": {
      const rows = await countRows();
      log.info("daily row counts", rows);
      return { scheduledAt: new Date(event.scheduledAt).toISOString(), rows };
    }
  }
}

// Counts the rows of every table the workflow may read. The workflow acts
// with its own grants (subject workflow:<id>), which an admin gives it.
async function countRows(): Promise<Record<string, number>> {
  const counts: Record<string, number> = {};
  for (const name of TABLE_NAMES) {
    try {
      const page = await tables(name).page({ limit: 1 });
      counts[name] = page.total;
    } catch (error) {
      if (!isPermissionError(error)) {
        throw error;
      }
      log.warn(`no read grant on table ${name}`, { error: error.message });
    }
  }
  return counts;
}
