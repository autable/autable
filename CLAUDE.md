# Claude Instructions

Read `README.md` (Development Rules) and `AGENTS.md` before making changes.

## No deployment's business may leak into this repository

autable is an open-source, general-purpose product. Whatever a private
deployment does with it — its table and field names, its form and workflow
scripts, company, vendor, or staff names, its document layouts, approval
chains, and ERP or IM specifics — must never appear here, **in any form**:
not in code, tests, fixtures, comments, sample data, locale strings, commit
messages, or docs.

This holds even when such a deployment is what motivated the feature. Describe
and test the capability, never the use case behind it.

- Invent neutral subjects for tests and examples: contacts, orders, widgets.
- Where a feature is genuinely about non-ASCII handling, keep non-ASCII
  coverage, but use plain words (`名称`, `数量`, `备注`) — never domain
  vocabulary borrowed from a real deployment.
- A feature request phrased in a deployment's terms gets implemented in the
  product's terms: generalize it first, then build the general thing.

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
