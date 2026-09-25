# Dependencies

Every direct dependency is justified here (Constitution VI). Versions are pinned by `go.sum` / `package-lock.json`; `govulncheck` and `npm audit --audit-level=high` run in CI.

| Dependency | Purpose | Alternatives rejected | Maintenance |
|------------|---------|-----------------------|-------------|
| `github.com/go-tangra/go-tangra/v4` (`replace ../..`) | mTLS transports, identity, service policy, audit, observability | — | this repository |
| `github.com/go-tangra/go-tangra-auth/v4` (`pkg/authclient`) | verify the platform token forwarded by the gateway; resolve members (`Profiles.ListMembers`) and decisions (`Authorization.Check`) | re-implementing JWT/revocation checks | this repository |
| `github.com/go-tangra/go-tangra-portal/v4` (`pkg/gatewayclient`, `api/schema`) | gateway registration, manifest types and schema | — | this repository |
| `github.com/jackc/pgx/v5` | TimescaleDB driver and pool (per-call tenant transactions, RLS) | database/sql + lib/pq | Active |
| `github.com/pressly/goose/v3` | embedded SQL migrations | golang-migrate | Active |
| `github.com/valkey-io/valkey-go` | live-event streams (`XADD`/`XREAD`) and rate-limit counters (research R4) | go-redis | Active |
| `github.com/getkin/kin-openapi` | OpenAPI parsing and per-route request validation for the browser API | manual validation per handler | Active |
| `github.com/santhosh-tekuri/jsonschema/v6` | validate the backup document against `backup.schema.json` and the manifest against the gateway schema | hand-written checks | Active |
| `github.com/golang-jwt/jwt/v5` | (test) mint platform tokens for the httpapi tests | — | Active |
| `google.golang.org/grpc`, `google.golang.org/protobuf` | the `notification.v1` service-to-service API | — | Google, active |
| `gopkg.in/yaml.v3` | configuration and policy parsing | — | Active |

**Standard library only** for the security-sensitive paths: `net/smtp` +
`crypto/tls` for email delivery (research R2), `mime`/`mime/quotedprintable`
for MIME assembly, `text/template`+`html/template` for rendering (research
R3), `crypto/aes`+`crypto/cipher` for the AES-256-GCM envelope (research R1).
No third-party SMTP, template, or crypto library is used.

## UI

| Dependency | Purpose |
|------------|---------|
| `vue`, `vue-router`, `pinia` | the SPA framework, routing and state |
| `vuetify`, `@mdi/font` | the Materio-styled component library |
| `@casl/ability`, `@casl/vue` | ability checks mirrored from the gateway's decisions |
| `@module-federation/vite` | build the remote consumed by the shell |
| `vitest`, `@vue/test-utils`, `jsdom` | unit tests |
| `@playwright/test`, `@axe-core/playwright` | end-to-end tests with accessibility checks |
| `openapi-typescript` | generate request/response types from the OpenAPI contract |
| `@tiptap/core`, `@tiptap/starter-kit`, `@tiptap/pm` | rich-text (WYSIWYG) editor for email template bodies (see below) |

### Template body editor (`@tiptap/*`, MIT)

- **Purpose**: the visual editor for email template bodies (bold, italic,
  underline, strike, headings, lists, quote, links, undo/redo) next to an HTML
  source view. Go template actions are cut out before the HTML reaches the
  editor and put back byte for byte afterwards (`ui/src/editor/`), so the
  library never escapes, reorders or splits them; bodies it cannot represent
  exactly stay in the source view.
- **Packages**: `@tiptap/core` (editor), `@tiptap/starter-kit` (the marks,
  nodes, link and history extensions, `trailingNode` off), `@tiptap/pm`
  (ProseMirror, which the kit is built on; pulls `linkifyjs` for the link
  extension). `@tiptap/vue-3` is **not** used: its only additions are menu
  components (and a `@floating-ui/dom` peer); the component mounts the core
  editor itself. `@tiptap/extension-text-align` was left out: alignment needs
  inline `style` attributes, which the console CSP (`style-src` without
  `unsafe-inline`) blocks in the editor, so it would not display.
- **Bundling**: bundled into the remote (not shared with the shell, no CDN;
  the CSP forbids external scripts) as a lazily loaded chunk fetched the first
  time an email template is opened (about 386 kB, 121 kB gzip). TipTap's
  injected `<style>` tag is disabled (`injectCSS: false`, CSP); the base
  ProseMirror rules are in `ui/src/main.css`.
- **Alternatives rejected**: Quill 2 (its Delta model normalizes HTML and has
  no stable way to keep `{{…}}` inside attributes; Vue wrappers are
  unmaintained), CKEditor 5 (GPL-2.0-or-later or a commercial licence, much
  larger), TinyMCE (GPL/commercial since v7, iframe-based, CDN-oriented),
  Lexical (no official Vue binding), a hand-written `contenteditable` +
  `document.execCommand` (deprecated API, inconsistent markup across browsers,
  no history or schema).
- **Maintenance**: TipTap (Tiptap GmbH) and ProseMirror (Marijn Haverbeke)
  are actively maintained, MIT-licensed, frequent releases (v3.31 in
  2026-09); versions pinned by `package-lock.json` and reviewed on every bump.
