# Changes to the gateway shell required by feature 006

## `./header` expose (header slot)

- `services/gateway/api/schema/manifest.schema.json`: `remote.exposes` items
  enum gains `"./header"` (also `internal/manifest` validation and
  `docs/module-guide.md`, `specs/003-application-gateway/contracts/federation.md`).
- `shell/src/federation/boot.ts`: after mounting routes, if the module's
  manifest lists `./header`, load it (`loadExpose<HeaderExpose>(module,
  './header')`) and register its default export in a `headerSlots` map
  (module → component); unmounting a module removes its slot. Failures are
  isolated like `./boot`.

```ts
/** What a remote's optional ./header expose must export. */
export interface HeaderExpose {
  /** Rendered in the app bar, right of the theme toggle, left of the avatar.
      Receives the same context as ./boot. Must render nothing (and open no
      connections) when the person lacks its permission. */
  default: Component  // props: { ability, session, api }
}
```

- `shell/src/layouts/Default.vue`: renders `<component v-for="[m, c] in
  headerSlots" :key="m" :is="c" v-bind="ctx" :data-test="'header-' + m" />`
  inside its own `RemoteBoundary` (a failing header component shows nothing,
  never breaks the shell). Order: module `order` of the first nav entry.
- Sign-out: the shell already performs a full navigation after sign-out, so
  header components need no explicit teardown; they must still close
  `EventSource`s in `onUnmounted`.
- Unit test (`tests/unit/header-slot.spec.ts`): a stubbed remote exposing
  `./header` renders in the app bar; a remote without it renders nothing; a
  throwing header component is isolated.
- E2E (`composition.spec.ts`): with the notification module registered, the
  bell is visible for a member and absent for a person without `inbox:read`.

## Allow-list

Development: `gatewaysvc bootstrap -allow
"spiffe://example.org/svc/notification=/api/notification,/ui;notification"`;
`services/gateway/deploy/dev.yaml` unchanged (the route timeout and body
limits come from the manifest).
