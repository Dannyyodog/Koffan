# Codebase Map — Koffan

Read-only reference for future sessions. Module path: `shopping-list` (see [go.mod:1](go.mod)). Targets Go 1.21, Fiber v2, SQLite (mattn/go-sqlite3), HTMX + Alpine.js + Tailwind on the frontend. Templates and static assets are `//go:embed`-ed into the binary.

Branch context: this map was written on `feat/item-images`, which currently differs from `main` only in the existence of this doc — no image-upload code has been added yet (see Open Questions).

## Entry point & routing

`main.go` is both the entry point and the route registry — there is **no separate router file**. Order of operations in [main.go:31-280](main.go):

1. [main.go:33-40](main.go) — `i18n.Init()` first (so DB migrations could use translations); applies `DEFAULT_LANG` env override.
2. [main.go:43-50](main.go) — `db.Init()`, `db.CleanExpiredSessions()`, `handlers.InitLoginRateLimiter()`.
3. [main.go:53-116](main.go) — Build the `gofiber/template/html/v2` engine over `embeddedTemplatesFS`, register custom funcs (`dict`, `add/sub/mul/div`, `gt/lt/eq/ne`, `T`, `toJSON`, `asset`).
4. [main.go:119-127](main.go) — `fiber.New` with `ViewsLayout: "layout"`; mounts `logger`, `recover`, `compress` middleware.
5. [main.go:130-160](main.go) — Hash the embedded static FS (`handlers.ComputeAssetHash`), build the service worker bytes, register `/static/sw.js` then `/static` filesystem middleware.
6. [main.go:163-170](main.go) — Register **pre-auth** routes (login + locales).
7. [main.go:171](main.go) — `api.Register(app)` mounts the optional REST API at `/api/v1/*` (see [api/api.go:10-70](api/api.go)).
8. [main.go:174](main.go) — `/api/version` (public).
9. [main.go:177](main.go) — `app.Use(handlers.AuthMiddleware)` — **everything below this line is session-gated** (the middleware also lets `/login` and `/static/...` pass, see [handlers/auth.go:142-145](handlers/auth.go)).
10. [main.go:180-189](main.go) — WebSocket upgrade gate + `/ws` handler.
11. [main.go:192-270](main.go) — All page + JSON/HTMX routes (listed below).
12. [main.go:273-279](main.go) — `PORT` env var (default `3000`), `app.Listen`.

### Routes

All paths below the `AuthMiddleware` line require a valid `session` cookie (or `DISABLE_AUTH=true`).

**Auth (pre-middleware)** — [main.go:163-165](main.go)
- `GET  /login` → `handlers.LoginPage`
- `POST /login` → `handlers.LoginRateLimitMiddleware` → `handlers.Login`
- `POST /logout` → `handlers.Logout`

**i18n / version (pre-middleware)** — [main.go:168, 174](main.go)
- `GET /locales` → `handlers.GetLocales`
- `GET /api/version` → `handlers.GetVersion`

**REST API v1 (pre-middleware, token-auth)** — [api/api.go:26-69](api/api.go). Registered only if `API_TOKEN` env var is set; otherwise all `/api/v1/*` returns 503. Uses `Authorization: Bearer <token>`.
- Lists: `GET/POST /api/v1/lists`, `GET/PUT/DELETE /api/v1/lists/:id`, `GET /api/v1/lists/:id/sections`, `POST /api/v1/lists/:id/move-up|move-down`
- Sections: `GET/POST /api/v1/sections`, `GET/PUT/DELETE /api/v1/sections/:id`, `GET /api/v1/sections/:id/items`, `POST /api/v1/sections/:id/move-up|move-down|check-all|uncheck-all|sort-mode`
- Items: `GET/POST /api/v1/items`, `GET/PUT/DELETE /api/v1/items/:id`, `POST /api/v1/items/:id/toggle|uncertain|quantity|move|move-up|move-down`
- Batch: `POST /api/v1/batch`
- History: `GET/POST /api/v1/history`, `DELETE /api/v1/history/:id`, `POST /api/v1/history/batch-delete`

**Pages** — [main.go:192-195](main.go)
- `GET /` → `handlers.GetListsPage` (renders `home`)
- `GET /lists/:id` → `handlers.GetListView` (renders `list`)

**WebSocket** — [main.go:180-189](main.go)
- `GET /ws` → `handlers.WebSocketHandler` (broadcasts mutation events to all clients)

**Sections** — [main.go:198-207](main.go)
- `GET    /sections/list` → `GetSectionsListForModal`
- `GET    /sections/:id/html` → `GetSectionHTML`
- `POST   /sections` → `CreateSection`
- `PUT    /sections/:id` → `UpdateSection`
- `DELETE /sections/:id` → `DeleteSection`
- `POST   /sections/:id/move-up` → `MoveSectionUp`
- `POST   /sections/:id/move-down` → `MoveSectionDown`
- `POST   /sections/:id/check-all` → `CheckAllItems`
- `POST   /sections/:id/uncheck-all` → `UncheckAllItems`
- `POST   /sections/:id/sort-mode` → `UpdateSectionSortMode`
- `POST   /sections/batch-delete` → `BatchDeleteSections` (declared in the "Batch operations" group at [main.go:259](main.go))

**Lists** — [main.go:210-218](main.go)
- `GET    /lists` → `GetLists` (HTML redirect, or JSON when `?format=json`)
- `POST   /lists` → `CreateList`
- `PUT    /lists/:id` → `UpdateList`
- `DELETE /lists/:id` → `DeleteList`
- `POST   /lists/:id/activate` → `SetActiveList`
- `GET    /lists/:id/activate` → `SetActiveList` (same handler, GET form for links)
- `POST   /lists/:id/move-up` → `MoveListUp`
- `POST   /lists/:id/move-down` → `MoveListDown`
- `POST   /lists/:id/toggle-completed` → `ToggleShowCompleted`

**Templates** — [main.go:221-230](main.go)
- `GET    /templates` → `GetTemplates`
- `GET    /templates/:id` → `GetTemplate`
- `POST   /templates` → `CreateTemplate`
- `PUT    /templates/:id` → `UpdateTemplate`
- `DELETE /templates/:id` → `DeleteTemplate`
- `POST   /templates/:id/items` → `AddTemplateItem`
- `PUT    /templates/:id/items/:itemId` → `UpdateTemplateItem`
- `DELETE /templates/:id/items/:itemId` → `DeleteTemplateItem`
- `POST   /templates/:id/apply` → `ApplyTemplate`
- `POST   /templates/from-list` → `CreateTemplateFromList`

**Items** — [main.go:233-243](main.go)
- `GET    /items/:id/html` → `GetItemHTML`
- `POST   /items` → `CreateItem`
- `POST   /items/delete-completed` → `DeleteCompletedItems`
- `PUT    /items/:id` → `UpdateItem`
- `DELETE /items/:id` → `DeleteItem`
- `POST   /items/:id/toggle` → `ToggleItem`
- `POST   /items/:id/quantity` → `AdjustItemQuantity`
- `POST   /items/:id/uncertain` → `ToggleUncertain`
- `POST   /items/:id/move` → `MoveItemToSection`
- `POST   /items/:id/move-up` → `MoveItemUp`
- `POST   /items/:id/move-down` → `MoveItemDown`

**Stats / offline / history** — [main.go:246-256](main.go)
- `GET    /stats` → `GetStats`
- `GET    /api/data` → `GetAllData`
- `GET    /api/item/:id/version` → `GetItemVersion`
- `GET    /api/suggestions` → `GetSuggestions`
- `GET    /api/history` → `GetHistory`
- `DELETE /api/history/:id` → `DeleteHistoryItem`
- `POST   /api/history/batch-delete` → `BatchDeleteHistory`

**Import / export** — [main.go:262-266](main.go)
- `GET  /export`, `GET /export/list/:id`, `GET /export/preview`
- `POST /import`, `POST /import/preview`

**DB management** — [main.go:269-270](main.go)
- `GET  /api/database/csrf-token` → `GenerateCSRFToken`
- `POST /api/database/clear` → `ClearDatabase`

## Database layer

**Schema location and approach.** Schema lives entirely inside `db/db.go` as Go string literals. There is **no migrations library**, no SQL files, no version table. The startup flow is:

1. `db.Init()` opens SQLite, then calls `createTables()` — [db/db.go:14-62](db/db.go).
2. `createTables()` runs one `CREATE TABLE IF NOT EXISTS` block for the original tables (`sections`, `items`, `sessions`, `item_history`) plus the original indexes — [db/db.go:64-109](db/db.go).
3. It then calls `runMigrations()` — [db/db.go:115-180](db/db.go) — which dispatches to a chain of single-purpose `migrateXxx` functions. Each one is idempotent: it queries `pragma_table_info` (or `sqlite_master`) to see whether a column/table already exists, and only runs the `ALTER TABLE` / `CREATE TABLE` if not.

So the codebase uses **two complementary patterns**:
- **Initial schema:** raw `CREATE TABLE IF NOT EXISTS` in the multi-statement `schema` string at [db/db.go:65-104](db/db.go).
- **Schema evolution:** a Go function per change, conditionally guarded by an existence probe, called from `runMigrations()`.

### Tables

`sections` — [db/db.go:66-72](db/db.go) (+ `list_id` from [db/db.go:216](db/db.go), `sort_mode` from [db/db.go:347](db/db.go))
- `id INTEGER PK AUTOINCREMENT`
- `name TEXT NOT NULL`
- `sort_order INTEGER NOT NULL`
- `created_at DATETIME DEFAULT CURRENT_TIMESTAMP`
- `updated_at INTEGER DEFAULT (strftime('%s','now'))`
- `list_id INTEGER REFERENCES lists(id) ON DELETE CASCADE` (added by migration)
- `sort_mode TEXT DEFAULT 'manual'` — values `'manual' | 'alphabetical' | 'alphabetical_desc'` (validated in [db/queries.go:486](db/queries.go))

`items` — [db/db.go:74-85](db/db.go) (+ `quantity` from [db/db.go:324](db/db.go))
- `id`, `section_id INTEGER NOT NULL FK→sections(id) ON DELETE CASCADE`
- `name TEXT NOT NULL`, `description TEXT DEFAULT ''`
- `completed BOOLEAN DEFAULT FALSE`, `uncertain BOOLEAN DEFAULT FALSE`
- `quantity INTEGER DEFAULT 0` (added by migration)
- `sort_order INTEGER NOT NULL`
- `created_at`, `updated_at` (same convention)

`sessions` — [db/db.go:87-90](db/db.go)
- `id TEXT PRIMARY KEY` (hex SHA, generated in [handlers/auth.go:43-49](handlers/auth.go))
- `expires_at INTEGER NOT NULL` (unix epoch seconds)

`item_history` — [db/db.go:92-99](db/db.go)
- `id`, `name TEXT NOT NULL COLLATE NOCASE`, `last_section_id INTEGER`
- `usage_count INTEGER DEFAULT 1`, `last_used_at INTEGER DEFAULT (strftime('%s','now'))`
- `UNIQUE(name COLLATE NOCASE)` — used for `INSERT … ON CONFLICT(name COLLATE NOCASE) DO UPDATE …`

`lists` — [db/db.go:198-209](db/db.go) (+ `icon` from [db/db.go:300](db/db.go), + `show_completed` from [db/db.go:370](db/db.go))
- `id`, `name TEXT NOT NULL`, `sort_order INTEGER NOT NULL`
- `is_active BOOLEAN DEFAULT FALSE`
- `created_at`, `updated_at`
- `icon TEXT DEFAULT '🛒'`, `show_completed BOOLEAN DEFAULT TRUE`

`templates` — [db/db.go:247-256](db/db.go)
- `id`, `name`, `description TEXT DEFAULT ''`, `sort_order`, `created_at`, `updated_at`

`template_items` — [db/db.go:264-274](db/db.go)
- `id`, `template_id INTEGER NOT NULL FK→templates(id) ON DELETE CASCADE`
- `section_name TEXT NOT NULL` (denormalized — templates store section by name, not id)
- `name`, `description`, `sort_order`, `created_at`

### Indexes
- `idx_items_section ON items(section_id, sort_order)` — [db/db.go:101](db/db.go)
- `idx_sections_order ON sections(sort_order)` — [db/db.go:102](db/db.go)
- `idx_item_history_name ON item_history(name COLLATE NOCASE)` — [db/db.go:103](db/db.go)
- `idx_lists_order`, `idx_lists_active` — [db/db.go:207-208](db/db.go)
- `idx_sections_list ON sections(list_id, sort_order)` — [db/db.go:223](db/db.go)
- `idx_templates_order` — [db/db.go:256](db/db.go)
- `idx_template_items_template ON template_items(template_id, sort_order)` — [db/db.go:275](db/db.go)

### Connection sharing
The `*sql.DB` is exposed as a **package-level global** `db.DB` ([db/db.go:12](db/db.go)). Handlers import `shopping-list/db` and call `db.GetItemByID(...)`, `db.CreateItem(...)`, etc. — there is no DI container, no `context.Context` plumbing, and no per-request DB handle.

WAL mode is enabled, but the pool is intentionally clamped to one connection (`SetMaxOpenConns(1)`, [db/db.go:42-44](db/db.go)) "to avoid SQLITE_BUSY errors and eliminate the need for retry logic on concurrent writes". Despite that, [handlers/items.go:15-31](handlers/items.go) defines a generic `retryOnBusy[T]` helper that wraps `db.MoveItemToSectionAtPosition` ([handlers/items.go:333](handlers/items.go)) — leftover from earlier concurrency work, used in only one place.

`busy_timeout=5000` ms is set both via the connection string and an explicit `PRAGMA` ([db/db.go:30, 53](db/db.go)).

`DB_PATH` env var picks the SQLite file (default `./shopping.db`); the parent directory is `MkdirAll`-ed on startup ([db/db.go:15-26](db/db.go)).

### Adding a new table or column — the existing pattern

Mimic this exactly. Do **not** introduce a migrations library.

**For a new column on an existing table** — copy the shape of `migrateItemQuantity` ([db/db.go:309-331](db/db.go)):

```go
func migrateItemQuantity() {
    var count int
    err := DB.QueryRow("SELECT COUNT(*) FROM pragma_table_info('items') WHERE name='quantity'").Scan(&count)
    if err != nil { log.Println("Migration check failed:", err); return }
    if count > 0 { return }
    log.Println("Running migration: Adding quantity to items...")
    _, err = DB.Exec("ALTER TABLE items ADD COLUMN quantity INTEGER DEFAULT 0")
    if err != nil { log.Println("Migration failed - adding quantity to items:", err); return }
    log.Println("Migration completed: Item quantity added")
}
```

Then add a single-line dispatch inside `runMigrations()` (e.g. [db/db.go:172-173](db/db.go): `// Migration: Add quantity to items \n migrateItemQuantity()`).

**For a new table** — copy `migrateTemplates` ([db/db.go:231-283](db/db.go)): probe `sqlite_master`, then `CREATE TABLE IF NOT EXISTS … ; CREATE INDEX IF NOT EXISTS …`. Again, register it in `runMigrations()`.

**For struct/query changes**, the corresponding scan code in `db/queries.go` uses `COALESCE(col, default)` defensively for every column added by a migration (e.g. `COALESCE(quantity, 0)` at [db/queries.go:587, 629](db/queries.go), `COALESCE(updated_at, 0)` everywhere). New nullable columns should follow this pattern so older rows don't break scans.

## Handler pattern

Pick: `CreateItem` at [handlers/items.go:34-117](handlers/items.go). This is "add item to list" — items are created against a section, and the active list is implicit via the section.

Signature:

```go
func CreateItem(c *fiber.Ctx) error
```

(All HTTP handlers in this codebase have the same `func(*fiber.Ctx) error` shape — Fiber's standard handler signature.)

Step by step:

1. **Parse + validate** — [handlers/items.go:35-53](handlers/items.go). `section_id` from `c.FormValue("section_id")` parsed via `strconv.ParseInt`; on error returns `sendError(c, 400, "error.invalid_section_id")`. `name` is required (400 `error.name_required`). `description` is optional. `quantity` is parsed as int and clamped to `>= 0`.
2. **Dedup check** — [handlers/items.go:55-89](handlers/items.go). Calls `db.FindItemByNameInSection(sectionID, name)`. Three branches:
   - Same name + completed → `db.ReactivateItem(...)`, broadcast `item_toggled`, set header `X-Item-Reactivated: true`, render `partials/item`.
   - Same name + active → set headers `X-Item-Already-Active: true` and `X-Item-Existing-ID`, return HTTP 200 with empty body so the client can highlight the existing row.
   - No match → continue.
3. **DB write** — [handlers/items.go:91-94](handlers/items.go). `db.CreateItem(sectionID, name, description, quantity)` returns the freshly-loaded `*db.Item`.
4. **Side effects** — [handlers/items.go:96-102](handlers/items.go).
   - `db.SaveItemHistory(name, sectionID)` updates the autocomplete index (uses `INSERT … ON CONFLICT(name) DO UPDATE`).
   - `BroadcastUpdate("item_created", item)` pushes a JSON event over WebSocket to every connected client.
   - `c.Set("HX-Trigger-After-Settle", '{"statsRefresh":"true"}')` tells HTMX to fire a client-side `statsRefresh` event after the swap settles, which refreshes the stats pill via the hidden `<div id="stats-container" hx-get="/stats" hx-trigger="refresh">` at [templates/list.html:260](templates/list.html).
5. **Response** — [handlers/items.go:104-116](handlers/items.go). Always renders **`partials/item`** (an HTML fragment, not JSON, not redirect). The third arg `""` to `c.Render` opts out of the layout wrapper. Both the regular form path and the `quick_add=true` path return the same partial; the client decides where to insert it. The data map always includes `Sections: getSectionsForDropdown()` so the rendered item carries a fresh "move to section" dropdown ([handlers/sections.go:198-201](handlers/sections.go)).

### Cross-cutting helpers
- `sendError(c, status, key)` — [handlers/i18n_helper.go:18-20](handlers/i18n_helper.go) — `c.Status(status).SendString(i18n.Get(getLang(c), key))`. Reads the `lang` cookie, falls back to default. Every handler returns errors through this — never raw `c.Status(...).SendString("hardcoded message")`.
- `getLang(c)` — [handlers/i18n_helper.go:10-15](handlers/i18n_helper.go).
- `getSectionsForDropdown()` — [handlers/sections.go:198-201](handlers/sections.go) — convenience to grab all sections of the active list when re-rendering an item partial.
- `sectionRenderMap(*db.Section)` — [handlers/lists.go:317-324](handlers/lists.go) — canonical data map for `partials/section` renders; bundles the section, the sections-for-dropdown list, and `ShowCompleted`.
- `BroadcastUpdate(eventType, payload)` — defined in `handlers/ws.go`, used by every mutating handler so all open tabs stay in sync.
- `retryOnBusy[T]` — [handlers/items.go:15-31](handlers/items.go) — generic SQLITE_BUSY retry; used only by the cross-section drag move at [handlers/items.go:333](handlers/items.go).

### Middleware

There is **no per-route middleware on the page/HTMX/WebSocket routes** beyond what the global `app.Use(...)` chain installs. The chain, in order, is:

- `logger.New()` — [main.go:125](main.go) — request log line per call.
- `recover.New()` — [main.go:126](main.go) — converts panics to 500.
- `compress.New(LevelBestSpeed)` — [main.go:127](main.go).
- `app.Use("/static", filesystem.New(...))` — [main.go:156-160](main.go) — mounts the embedded static FS for any path under `/static/` (with `MaxAge: 30 days`).
- `app.Use("/ws", upgrade-gate)` — [main.go:180-186](main.go) — rejects non-WebSocket requests to `/ws`.
- `app.Use(handlers.AuthMiddleware)` — [main.go:177](main.go) — session-cookie check, see below.

The auth middleware is the only auth layer for HTMX/page routes:

- `handlers.AuthMiddleware` — [handlers/auth.go:136-206](handlers/auth.go). Honors `DISABLE_AUTH=true`; lets `/login` and `/static/...` through; loads the session row, validates expiry, and on failure either responds with `HX-Redirect: /login` + `401` (when `HX-Request: true`) or a normal `302` redirect.

Two scoped middlewares exist outside the global chain:
- `handlers.LoginRateLimitMiddleware` — applied **per-route** to `POST /login` only ([main.go:164](main.go), implementation [handlers/ratelimit.go:156-173](handlers/ratelimit.go)). In-memory map keyed by `c.IP()`, configurable via `LOGIN_MAX_ATTEMPTS`, `LOGIN_WINDOW_MINUTES`, `LOGIN_LOCKOUT_MINUTES`.
- `api.TokenAuthMiddleware` — applied to the whole `/api/v1` group ([api/api.go:26](api/api.go), implementation [api/middleware.go:21-55](api/middleware.go)). Validates `Authorization: Bearer <API_TOKEN>`.

## Templates & frontend

### Layout

Templates live in `templates/` and are loaded via `gofiber/template/html/v2` over an `embed.FS` rooted at `templates/` ([main.go:53-58](main.go)). The Fiber app is configured with `ViewsLayout: "layout"` ([main.go:122](main.go)), so every `c.Render("name", data)` call wraps the named template inside `templates/layout.html`'s `{{embed}}` placeholder ([templates/layout.html:432](templates/layout.html)). Passing an explicit empty string as the third arg to `c.Render` (e.g. `c.Render("partials/item", data, "")`) **opts out of the layout** — used for every HTMX fragment response.

There is no automatic template discovery for partials — every file uses `{{define "name"}}…{{end}}`. Top-level pages define `home`, `list`, `lists`, `login`. Partials in `templates/partials/` define `partials/item`, `partials/section`, etc. and are invoked via `{{template "partials/item" dict "Item" $item "Sections" $.Sections}}`. The `dict` template func ([main.go:63-76](main.go)) lets templates synthesize maps inline because `html/template` has no native map literal.

### File inventory
- Top-level pages: [home.html:1](templates/home.html) (`{{define "home"}}` — homepage with all lists), [list.html:1](templates/list.html) (`{{define "list"}}` — the main shopping view, ~1100 lines), [lists.html:1](templates/lists.html) (legacy lists view), [login.html:1](templates/login.html), [layout.html:1](templates/layout.html) (the wrapping shell).
- Partials: `partials/item.html`, `partials/item_completed.html`, `partials/list_item.html`, `partials/lists_container.html`, `partials/manage_section_item.html`, `partials/manage_sections_list.html`, `partials/section.html`, `partials/sections_list.html`, `partials/stats.html`, `partials/template_item.html`, `partials/templates_list.html`.

Naming convention: top-level templates are bare (`home`, `list`, `login`); partial names are namespaced (`partials/item`).

### How a single item renders

`templates/partials/item.html` is the row partial (147 lines). Quoting the relevant first chunk — note the `data-item-id` / `data-section-id` (used by SortableJS), the `x-show="isItemVisible(...)"` Alpine binding, and the `Quantity` badge:

```html
<div
    id="item-{{.Item.ID}}"
    data-item-id="{{.Item.ID}}"
    data-section-id="{{.Item.SectionID}}"
    class="px-4 py-3 flex items-center gap-0.5 hover:bg-stone-50 dark:hover:bg-stone-700 transition-colors group select-none {{if .Item.Uncertain}}bg-amber-50/50 dark:bg-amber-900/30{{end}}"
    x-show="isItemVisible({{.Item.ID}})"
>
    <!-- Drag Handle, Checkbox, Content (clickable to toggle) -->
    …
    <p class="item-name text-sm text-stone-700 dark:text-stone-200 truncate">{{.Item.Name}}</p>
    {{if gt .Item.Quantity 0}}
    <span class="px-1.5 py-0.5 text-xs font-medium bg-stone-100 …">{{.Item.Quantity}}x</span>
    {{end}}
```

See [templates/partials/item.html:1-46](templates/partials/item.html) for the full rendering. Items are nested inside a section partial that loops over them at [templates/partials/section.html:175-179](templates/partials/section.html):

```html
<div class="divide-y divide-stone-100 dark:divide-stone-700 active-items items-sortable" data-section-id="{{.Section.ID}}">
    {{range .Section.Items}}
    {{if not .Completed}}
    {{template "partials/item" dict "Item" . "Sections" $.Sections}}
    {{end}}
    {{end}}
</div>
```

### HTMX usage

HTMX is loaded globally in the layout ([templates/layout.html:129](templates/layout.html)). Despite that, **only four templates** actually use `hx-*` attributes: `templates/list.html`, `templates/lists.html`, `templates/partials/stats.html`, `templates/partials/template_item.html`. Most mutations are driven from Alpine via `fetch()` / `htmx.ajax(...)` rather than declarative `hx-*` attributes.

A concrete declarative example — the "Add new section" form inside the manage-sections sheet ([templates/list.html:452-466](templates/list.html)):

```html
<form
    hx-post="/sections"
    hx-swap="none"
    hx-on::after-request="if(event.detail.successful) { this.reset(); … sl.insertAdjacentHTML('beforeend', html.trim()); … }"
    hx-on::response-error="handleSectionFormError(this, event.detail.xhr)"
    class="flex flex-col gap-2 mb-6"
>
```

The handler returns a `partials/section` fragment, and the `hx-on::after-request` glue inserts it into `#sections-list`. The same pattern shows up for stats refresh — there's a hidden trigger element ([templates/list.html:260](templates/list.html)):

```html
<div id="stats-container" class="hidden" hx-get="/stats" hx-trigger="refresh" hx-swap="none"></div>
```

…which gets fired by handlers via `c.Set("HX-Trigger-After-Settle", '{"statsRefresh":"true"}')` ([handlers/items.go:79, 102, 156, 201](handlers/items.go) etc.) plus a small Alpine listener that maps `statsRefresh` → `htmx.trigger('#stats-container', 'refresh')`.

A second pattern is the imperative `htmx.ajax(...)` call from inside Alpine — section inline-rename uses it at [templates/partials/section.html:23](templates/partials/section.html):

```html
@submit.prevent="htmx.ajax('PUT', '/sections/{{.Section.ID}}', {values: {name: editSectionName}, target: '#section-{{.Section.ID}}', swap: 'outerHTML'}).then(() => editingSection = false)"
```

So when planning UI changes: prefer following the existing **`hx-` attribute + `hx-on::after-request` JS shim** style for forms, or **Alpine `fetch()` + per-item partial render** for in-place updates. Both styles are already in the codebase; pick whichever matches the surrounding section.

### Alpine.js

Alpine and the `collapse` plugin are loaded in the layout ([templates/layout.html:133-134](templates/layout.html)). Components are defined in three places:
- **`shoppingList()`** — the big list-view component, defined in [static/app.js:208](static/app.js). Wired by `<div x-data="shoppingList()" x-init="init()" …>` at [templates/list.html:2](templates/list.html). Holds online/offline state, websocket handler, sortable wiring, mobile sheets, suggestions cache, etc.
- **`homePage()`** — defined inline at [templates/home.html:668](templates/home.html), wired at [templates/home.html:2](templates/home.html).
- **`listsPage()`** — defined inline at [templates/lists.html:214](templates/lists.html), wired at [templates/lists.html:2](templates/lists.html).

There is no `Alpine.data(...)` registration — the components are plain global functions evaluated lazily by Alpine when it sees the matching `x-data="…()"`.

## Static assets

Served via the Fiber `filesystem` middleware over the `embed.FS` rooted at `static/`. Mount and behavior:

```go
//go:embed static/*
var embeddedStaticFS embed.FS
…
app.Use("/static", filesystem.New(filesystem.Config{
    Root:   http.FS(staticRootFS),
    Browse: false,
    MaxAge: 86400 * 30, // 30 days
}))
```

— [main.go:28-29, 156-160](main.go). Route prefix is **`/static/`**. Files are served directly from the embedded FS with a 30-day `Cache-Control: max-age` and (per-binary) immutable content because they're embedded at build time. The service worker is the one exception: it's pre-built into `handlers.ServiceWorkerBytes` at startup ([main.go:145-149](main.go), [handlers/assets.go:59-66](handlers/assets.go)) and served by a dedicated handler with `Cache-Control: no-cache` at `/static/sw.js` ([main.go:154](main.go), [handlers/assets.go:69-76](handlers/assets.go)). That's wired *before* the filesystem `Use` so the dedicated handler wins.

A content hash over the entire static FS is computed once at startup (`handlers.ComputeAssetHash`, [handlers/assets.go:26-55](handlers/assets.go)) and exposed in templates as `{{asset "foo.js"}}` → `/static/foo.js?v=<hash>` (template func at [main.go:113-115](main.go)). The same hash is substituted into `__CACHE_VERSION__` / `__ASSET_HASH__` placeholders inside `static/sw.js` so any asset change auto-busts both browser and SW caches.

### Serving files from outside the embedded FS

There is **no existing pattern for serving files from a different on-disk directory** (e.g. user-uploaded images stored under `/data/`). Everything served at `/static/` comes from the embedded FS that's baked into the binary at compile time; there is no second `filesystem.New` mount, no `app.Static(...)` call, and no streaming/upload handler in the codebase today. The only files written to disk by the app are the SQLite database file and its WAL/SHM (`DB_PATH`, default `./shopping.db`).

If image uploads need to be served, a new mount will have to be added. The closest parallel in the codebase is the existing `filesystem.New(...)` mount at [main.go:156-160](main.go) — but it would need to point at `http.Dir(uploadsRoot)` instead of `http.FS(staticRootFS)`, and it would need to live **after** `AuthMiddleware` if uploads are private. (See Open Questions.)

## Internationalization

Self-contained in the `i18n/` package.

**Storage.** One JSON file per language, sitting in `i18n/`: `en.json`, `pl.json`, `de.json`, `es.json`, `fr.json`, `it.json`, `pt.json`, `cs.json`, `sk.json`, `ru.json`, `nl.json`, `sv.json`, `no.json`, `lt.json`, `el.json`, `fa.json`, `ua.json`, `zh.json`. Each file starts with a `meta` block (`code`, `name`, `flag`) and then nested string maps (`common.add`, `items.what_to_buy`, `confirm.delete_item`, etc.). See [i18n/en.json:1-60](i18n/en.json) and the contributor guide at [i18n/README.md:1-174](i18n/README.md).

**Loading.** All JSON files are `//go:embed *.json`-ed into the binary ([i18n/locales.go:11-12](i18n/locales.go)). `i18n.Init()` ([i18n/locales.go:61-108](i18n/locales.go)) is called first thing in `main()` ([main.go:33](main.go)); it walks the embedded FS, parses each file into `map[string]interface{}`, indexes the `meta.code`, and builds a one-shot `cachedAllLocales` map.

**Default language.** `defaultLang` defaults to `"en"`; can be overridden by `DEFAULT_LANG` env var ([main.go:38-40](main.go), [i18n/locales.go:45-51](i18n/locales.go)). The user's per-request language comes from a `lang` cookie (`getLang(c)` at [handlers/i18n_helper.go:10-15](handlers/i18n_helper.go)).

**Server-side use.** Handlers call `i18n.Get(lang, key)` (or `sendError` which wraps it) to render error strings. Page handlers also push the *full* set of translations into the template data map so the client has them all in memory:

```go
return c.Render("home", fiber.Map{
    "Lists":        lists,
    "Translations": i18n.GetAllLocales(),
    "Locales":      i18n.AvailableLocales(),
    "DefaultLang":  i18n.GetDefaultLang(),
})
```

— [handlers/lists.go:33-39](handlers/lists.go). Inside templates, the `T` function is registered as a func map entry ([main.go:105](main.go)) for server-side translation, e.g. `{{T .Lang "list.title"}}`.

**Client-side use.** The layout serializes the translations into JS globals at [templates/layout.html:34-37](templates/layout.html):

```html
window.translations = {{.Translations | toJSON}};
window.locales = {{.Locales | toJSON}};
window.defaultLang = {{.DefaultLang | toJSON}};
```

…and exposes a `t(key, params)` helper ([templates/layout.html:54-76](templates/layout.html)) used everywhere in templates and Alpine code (`x-text="t('items.what_to_buy')"`, `:title="t('actions.move')"`, `confirm(t('confirm.delete_item', {name: ...}))`, etc.). Parameter substitution uses `{{paramName}}` placeholders; see the `t()` regex at [templates/layout.html:71-73](templates/layout.html).

**Adding new strings.** Edit every `i18n/*.json` file (or at minimum `en.json` — the `Get()` lookup falls back to the key string if missing). No code changes required because both the server template func and the client `t()` helper traverse the JSON tree dynamically. After changing JSON, you must rebuild because the files are embedded into the binary ([i18n/README.md:42-49](i18n/README.md)).

## Tests

The `test/` directory contains exactly **one file**: [test/viewport-height.test.js](test/viewport-height.test.js) — a 54-line Node.js test for `static/viewport.js` using the Node built-in test runner (`node:test` + `node:assert/strict`). No Go test files exist in the repo (`*_test.go` is absent in `db/`, `handlers/`, `api/`, `i18n/`, root). There is no `go test` target, no testify, no httptest harness, no `Makefile`.

Sample shape from the existing test:

```js
const test = require('node:test');
const assert = require('node:assert/strict');
const { getVisibleViewportHeight, syncViewportHeight } = require('../static/viewport.js');

test('getVisibleViewportHeight prefers visualViewport height when available', () => {
    const win = { innerHeight: 780, visualViewport: { height: 512 } };
    assert.equal(getVisibleViewportHeight(win), 512);
});
```

There is **no existing example to follow when adding tests for Go handlers**. If we want handler tests for new image-upload code, the project doesn't currently have a convention — we'd be establishing one. (See Open Questions.)

## Conventions to follow

### Commit messages
Conventional-Commits-style prefixes, observed across the last 30 commits:
- `feat: …` — new functionality (`feat: add quantity selector to add item form`, `feat: auto cache-bust static assets by content hash`)
- `fix: …` — bugfix (`fix: add missing translations and fix Italian locale typo`, `fix: escape PORT in compose healthcheck …`)
- `chore: …` — version bumps and other housekeeping (`chore: bump version to 2.11.0`)
- `refactor: …`, `perf: …`, `docs: …` — used as expected (`refactor: extract sectionRenderMap helper …`, `perf: reduce query count, add compression and static caching`, `docs: add viewport fix design spec`)

Subject lines are lowercase after the colon, no trailing period, generally under ~70 chars. Squash-merge PRs from contributors sometimes appear without the prefix (e.g. `Add Italian locale`, `Add Chinese (Simplified) language support to i18n (#113)`) — those are external contributions that were merged as-is. When making commits in this branch, use the conventional prefix.

### Branches
The two non-main branches in the remote are `feat-home-assistant-addon` and `feat/item-images`. So both `feat-foo` and `feat/foo` styles exist; the current branch (`feat/item-images`) uses the slash form. New feature branches should follow `feat/<short-name>`.

### Code style notes
- **Error handling in handlers.** Always `return sendError(c, status, "i18n.key")` for user-facing errors rather than `c.SendString(...)` with a literal — keeps everything translatable. Internal logs use `log.Printf("…: %v", err)`.
- **DB access from handlers.** Always go through the `shopping-list/db` package; never call `db.DB.Exec(...)` directly from a handler. New queries belong in `db/queries.go` next to the related ones, in the existing `// ==================== <SECTION> ====================` banners.
- **Mutating handlers must broadcast.** Every successful state-change calls `BroadcastUpdate("<event>", payload)` so other tabs sync. Examples: `item_created`, `item_updated`, `item_toggled`, `item_moved`, `item_deleted`, `section_created`, `list_created`. Pick a parallel name for new mutations.
- **HTML fragment responses use `c.Render("partials/foo", data, "")`** with an explicit empty third arg to skip the layout. Pages omit the third arg.
- **Stats refresh after mutation.** When the change affects per-list counts, set `c.Set("HX-Trigger-After-Settle", '{"statsRefresh":"true"}')` before returning.
- **Input length limits live in [handlers/lists.go:16-22](handlers/lists.go)** as package constants (`MaxListNameLength`, `MaxIconLength`, `MaxSectionNameLength`, `MaxItemNameLength`, `MaxDescriptionLength`). Validate against these in any new write handler.
- **Logging style.** Tag-prefixed log lines like `log.Printf("[AUTH] …")`, `log.Printf("[RATE LIMIT] …")` are common. Match the convention if adding a new subsystem.
- **No global router file** — keep route registration in `main.go`, grouped by resource with the existing `// Foo API` comment headers. Do not introduce a separate `routes.go` for image upload; add the routes inside the existing structure.

## Open questions

- **No file-upload pattern exists today.** No handler in the codebase currently parses `multipart/form-data` (a grep for `FormFile`, `MultipartForm`, `multipart` in Go code returns nothing). We'll need to choose: Fiber's `c.FormFile(...)` + `c.SaveFile(...)` for one-shot uploads, or `c.MultipartForm()` for multi-file. Worth deciding before the next session.
- **No on-disk asset serving exists today.** Everything under `/static/` is from the embedded FS. Serving uploaded images from `/data/images/` (or wherever) requires adding a *new* `filesystem.New(...)` mount with `Root: http.FS(os.DirFS(uploadsDir))` (or `http.Dir(...)`). Decide:
  1. URL prefix — `/uploads/`, `/images/`, `/data/`?
  2. On-disk root — env var (`UPLOADS_PATH`?) parallel to `DB_PATH`, defaulting to `./uploads/` or sibling of the DB file?
  3. Auth — should image fetches go *through* `AuthMiddleware`? The existing `/static/` mount is intentionally pre-auth ([handlers/auth.go:142-145](handlers/auth.go)); user-uploaded photos almost certainly should *not* be.
- **No migrations primitive for new tables/columns abstractly named.** Pattern is "one Go function per change, dispatched from `runMigrations()`". For an `image_path` column on `items` we'd add `migrateItemImage()` and a single line in `runMigrations()` — but worth confirming the team is happy adding more functions to `db/db.go` rather than introducing a migrations table/library.
- **No Go test harness.** The only test is a JS test for a static asset. Decide whether image-upload handlers should ship with `*_test.go` (and if so, set the convention now), or whether testing remains manual-only.
- **`retryOnBusy` exists but is largely unused.** It's defined in [handlers/items.go:15-31](handlers/items.go) and used only at [handlers/items.go:333](handlers/items.go), even though `MaxOpenConns=1` is supposed to make retries unnecessary. Don't proliferate it for image uploads unless a concrete BUSY scenario shows up.
- **WebSocket broadcast schema is informal.** Event names (`item_created`, `item_updated`, etc.) are string literals scattered across handlers; there is no central enum or message-type registry. Picking a name like `item_image_updated` is fine — just keep the snake_case convention and mirror the payload shape (`map[string]int64{"id":..., "section_id":...}` or a full struct).
- **`GetItemHTML` already serves a single-item HTML refresh** ([handlers/items.go:413-433](handlers/items.go), route `GET /items/:id/html` at [main.go:233](main.go)). After an image upload completes, this endpoint is the most natural way to refresh the item row — confirm in the next session that we want to reuse it rather than introduce a new `/items/:id/image` HTML endpoint.
- **`DISABLE_AUTH=true` exists** ([handlers/auth.go:28-30](handlers/auth.go)). Image upload should still be safe under that mode (it's a dev escape hatch), but we should check that uploads written under the bypass don't leak to other tenants in shared deployments.
- **The codebase has no concept of a "user."** Sessions are anonymous (just `id` + `expires_at`); the app password is shared. So any image upload is implicitly attributable to "whoever logged in with the shared password." There is no user-scoping concept to attach images to — they just belong to an item.
