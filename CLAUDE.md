# Claude Instructions

Read `README.md` (Development Rules) and `AGENTS.md` before making changes.

## Database schema changes REQUIRE a migration — no exceptions

The product is live. Every change to a GORM model in `internal/systemdb` —
including tables that did not exist in the last release, because interim
builds may already have created them — must answer this question before it
ships: **what does AutoMigrate do to a database that already has the old
shape and data?**

- AutoMigrate only handles: creating missing tables, adding nullable
  columns, adding columns with a `default` tag, adding indexes.
- Everything else — new NOT NULL columns without defaults, changed column
  types, renamed columns, re-keyed tables, data backfills — MUST ship as a
  versioned migration in `internal/systemdb/migrations.go` (`migrations[i]`
  upgrades schema version i to i+1, runs unconditionally in a transaction,
  raw SQL allowed there and only there).
- Never edit or reorder released migrations; append only.
- Every migration needs a test in `internal/systemdb/migrations_test.go`
  that builds the OLD shape with the OLD GORM model (never hand-written
  DDL) plus real rows, then proves `Open` upgrades it with data intact.

This rule has been violated twice (`workflow_models.runners_json`,
`runner_token_models.database_name`), both times crash-looping the live
deployment with "Cannot add a NOT NULL column with default value NULL".
Do not make it a third.
