# Apps: hosted web bundles

Status: in progress. Replaces forms.

## Why

Forms were JavaScript scripts that built a page through a small DSL
(`render(api, root)`). Anything the DSL did not cover had to be forced
through raw HTML, which is harder to maintain than an ordinary frontend.

An app is an ordinary frontend project: React, Vue, plain TypeScript, any
libraries. A coding agent writes it on a developer machine, `autablectl`
builds and uploads it, and autable serves it from the same origin as its
API. Data access goes through the existing HTTP API with the signed-in
user's own permissions, so an app can never see more than its user can.

## Model

An app belongs to a database and has:

| Field | Meaning |
|---|---|
| `name` | URL slug, `^[a-z0-9][a-z0-9-]{0,62}$`, unique per database |
| `title` | Display name, any text |
| `published_version` | The version users get; `0` while nothing is published |

A **version** is an immutable upload, numbered 1, 2, 3… per app:

| Field | Meaning |
|---|---|
| `version` | Sequence number within the app |
| `sdk` | SDK version the app was built against, `MAJOR.MINOR` |
| `message` | Free text from the uploader (what changed) |
| `bundle` | The uploaded archive (see below) |
| `size`, `created_by`, `created_at` | Bookkeeping |

Versions live as blobs in the system database (`app_version_models`), so
they are covered by the existing system database backup and need no new
storage backend. Pushing never changes what users see; publishing moves
`published_version`. Rolling back is publishing an older version.

### Bundle

A gzip-compressed tar archive:

```
autable.app.json     manifest
dist/...             built static files; dist/index.html is required
source/...           the project source, without node_modules or build output
```

The source travels with every version so whoever maintains an app next
can pick it up: `autablectl app pull` downloads the source of any version
(app writers only), and rolling back the build also identifies the
matching source. Unknown manifest fields are ignored, so newer CLI
releases can add fields without breaking older servers.

Limits: 30 MiB compressed, 200 MiB uncompressed, 5000 entries. Entry
paths must be relative, clean, and free of `..`; links and devices are
rejected.

### Manifest

`autable.app.json` sits in the project root and is copied into the bundle:

```json
{ "database": "sales", "name": "orders", "title": "Orders", "sdk": "1.0" }
```

`autablectl app sdk` writes the `sdk` field together with the SDK files,
so the field always describes the SDK actually vendored into the project.

## SDK and compatibility

The SDK is a small TypeScript module that wraps the HTTP API: tables
(list, query, page, get, create, update, upsert, delete, history), files,
users, the signed-in user, and the app's own base path. `autablectl app
sdk` also generates `schema.ts` from the database metadata, so table and
field names are type-checked; a removed or renamed field is a compile
error, not a runtime surprise.

The server embeds the SDK source and serves it at `GET /api/apps/sdk`;
`autablectl` vendors it from the server it talks to, so a project always
carries the SDK matching that server.

The SDK version is `MAJOR.MINOR`:

- **MINOR** adds API without breaking older apps.
- **MAJOR** changes or removes API. A server supports a range of majors,
  `[MinMajor, CurrentMajor]`, and drops old majors only deliberately.

A version is **supported** when its major is in the range and, for the
current major, its minor is not newer than the server's. The server
enforces this at every entry point:

| Where | Unsupported result |
|---|---|
| Upload | `422` with the supported range; nothing is stored |
| Publish | `422`; the published version is unchanged |
| Opening the app | `410` page: the app needs to be rebuilt with a newer SDK |
| API calls carrying `X-Autable-SDK` | `410` JSON error, so a tab left open across an upgrade stops instead of misbehaving |

The web UI marks unsupported versions so owners see which apps need work
before or after an upgrade.

## URLs and serving

| URL | Who | Serves |
|---|---|---|
| `/apps/{database}/{name}/` | app readers | the published version |
| `/apps/{database}/{name}/@{version}/` | app writers | any version (preview before publishing) |

- Requests without a session redirect to `/login?redirect=…`.
- Paths not found in `dist/` fall back to `dist/index.html` (client-side
  routing), except paths whose last segment has a dot, which 404.
- The server inserts `<base href="/apps/{database}/{name}/[@{version}/]">`
  into `index.html`. Projects build with a relative base (`base: "./"` in
  Vite), so the same build works published and in preview; the SDK reads
  `document.baseURI` for the router base path.
- Every file is sent with `Cache-Control: no-cache` and an ETag per
  version and path, since the same path may belong to different
  versions; revalidation is a cheap 304.

## Permissions

Scopes mirror the former form scopes:

| Scope | Resource | Read | Write |
|---|---|---|---|
| `app_set` | database name | open every app in the database | push, publish and roll back every app; create apps |
| `app` | app id | open the app | push, publish and roll back the app |

The database owner can do everything, including deleting apps. The user
who creates an app gets `app` Write on it. Data access inside an app is
checked per request against the user's own table, field, view and record
grants, exactly as in the autable UI.

## API

```
GET    /api/apps/sdk                                    {version, min_major, files}
GET    /api/databases/{db}/apps                         apps visible to the user
GET    /api/databases/{db}/apps/{name}                  app with its versions
POST   /api/databases/{db}/apps/{name}/versions         body: bundle; ?message=&publish=1
POST   /api/databases/{db}/apps/{name}/publish          {"version": N}
GET    /api/databases/{db}/apps/{name}/versions/{v}/source   source as tar.gz
DELETE /api/databases/{db}/apps/{name}                  owner only
```

Uploading to a name that does not exist creates the app (needs the create
permission). The manifest's `database` and `name` must match the URL.

## Command line

```
autablectl app init DIR --database DB --name NAME [--title T]
autablectl app sdk [DIR]                 refresh SDK and schema types
autablectl app dev [DIR]                 dev server proxied to autable with your token
autablectl app push [DIR] [-m MSG] [--publish] [--no-build]
autablectl app list DATABASE
autablectl app versions DATABASE NAME
autablectl app publish DATABASE NAME VERSION
autablectl app rollback DATABASE NAME    publish the newest version below the current one
autablectl app pull DATABASE NAME DIR [--version N]
```

`push` runs `npm run build` (unless `--no-build`), packs `dist/` and the
source, and prints the new version and its preview URL. Source files are
taken from `git ls-files --cached --others --exclude-standard` when the
project is a git repository, otherwise everything except `node_modules`,
`dist`, `.git` and `.autable`.

## Removing forms

- Form routes, the form runtime, the published-form page, form commands in
  `autablectl`, and the `form` / `form_set` scopes are removed.
- Migration 5 → 6 deletes `form` and `form_set` grants and drops
  `form_models`. Form scripts already written to the repository under
  `form/` are left untouched, and the repository sync no longer stages
  that directory. Rewrite forms as apps before upgrading a deployment that
  uses them.
