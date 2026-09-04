# PV T06 — settings

## Contract

Rebuild `apps/web/app/pages/app/settings/sessions.vue` and the blocks it hosts
into one readable page, and make the theme cookie hold across server renders.

### Utilities

`apps/web/app/utils/userAgent.ts`:

```ts
export function describeUserAgent(ua: string): string;
```

Returns "{Browser} {major} on {OS}" using, in order: Edge (`Edg/`), Chrome
(`Chrome/` without `Edg/`), Firefox (`Firefox/`), Safari (`Safari/` with
`Version/`), else "Unknown browser"; OS from `Windows`, `Mac OS X`, `Android`,
`iPhone`/`iPad` as "iOS", `Linux` (checked after Android), else omitted.
Headless Chrome reads "Chrome {major} on Linux". Never throws; empty or
non-string input returns "Unknown browser". Uses `formatRelativeTime` from T05
for "Last seen"; if T05 has not landed, create the same file with the same
contract and the owner dedupes at integration.

### Page

```vue
<main class="mx-auto w-full max-w-3xl px-6 py-10" data-testid="settings-page">
  <h1 class="text-xl font-semibold">Settings</h1>
  <section aria-labelledby="devices-title" class="border-t py-8">
    <h2 id="devices-title" class="text-lg font-semibold">Signed-in devices</h2>
    <ul class="mt-4 divide-y">
      <li v-for="session in sessions" :data-testid="`session-row-${session.id}`" class="grid grid-cols-[1fr_auto] gap-4 py-3">
        <div>
          <span>{{ describeUserAgent(session.ua) }}</span>
          <span v-if="session.current" class="ml-2 text-xs text-muted-foreground">This device</span>
          <span class="block text-xs text-muted-foreground tabular-nums">Last seen {{ formatRelativeTime(session.lastSeenAt, now) }}</span>
        </div>
        <Button v-if="session.current" variant="secondary" @click="logout">Log out</Button>
        <Button v-else variant="secondary" @click="revoke(session.id)">Revoke</Button>
      </li>
    </ul>
    <Button variant="outline" class="mt-4 text-destructive" @click="logoutEverywhere">Log out everywhere</Button>
  </section>
  <section aria-labelledby="password-title" class="border-t py-8"><PasswordSettings /></section>
  <section v-if="agentAccess" aria-labelledby="agents-title" class="border-t py-8"><ConnectedAgents /></section>
  <section v-if="providerLogin" aria-labelledby="providers-title" class="border-t py-8">…existing linking block…</section>
</main>
```

Error and reauth prompts render as `StatusBanner` blocks at the top of the
section they belong to, keeping their `data-testid`s. `PasswordSettings` and
`ConnectedAgents` keep their logic; restyle their markup to the section grammar
(title `lg`, ruled rows, secondary buttons, destructive-outline for revoke-all
style actions). The full user-agent string moves to a `title` attribute on the
description.

### Theme at server render

`app.vue` sets `data-theme` on `<html>` from the `aboutme-theme` cookie at
server render for every route where `isAppSurface` is true (PU's `useTheme`
already reads the cookie on the client; add the `useHead({ htmlAttrs })` read on
the server). Add `/app/settings/**` to `isAppSurface` if the rebased `app.vue`
lacks it.

Strings held: "Signed-in devices", "This device", "Log out", "Revoke", "Log out
everywhere", "Change password", "You have a password.", and every string
asserted by `test/password-settings.test.ts`, `test/connected-agents.test.ts`,
`test/sessions*.test.ts`, `auth.spec.ts`, and `mcp.spec.ts`. Hook change: the
row text no longer contains the raw user-agent string; tests that matched it now
match `describeUserAgent`'s output.

## TDD cases

Write `test/app/user-agent.test.ts` first with fixtures for Chrome on Linux,
HeadlessChrome, Edge on Windows, Firefox on macOS, Safari on iPhone, Android
Chrome, empty string, a 4 KB string, and non-ASCII. Write
`test/sessions-settings.test.ts`: rows show the description, relative time with
an injected `now`, "This device" only on the current row, one Log out and n−1
Revoke buttons, the raw UA in `title`; the agents section renders only when
`agentAccess`; the providers section only when `providerLogin`; a server render
with cookie `aboutme-theme=dark` yields `data-theme="dark"` on `<html>`.

## Ownership and checks

Owned paths:

- `apps/web/app/pages/app/settings/sessions.vue`
- `apps/web/app/components/auth/PasswordSettings.vue`
- `apps/web/app/components/settings/ConnectedAgents.vue`
- `apps/web/app/utils/userAgent.ts`
- `apps/web/app/app.vue` (theme read only; the owner merges with T07's edit)
- `apps/web/test/app/user-agent.test.ts`, `test/sessions-settings.test.ts`,
  `test/password-settings.test.ts`, `test/connected-agents.test.ts`

Acceptance: `AC-UI-010`.

Run:

```sh
cd apps/web
npx vitest run test/password-settings.test.ts test/connected-agents.test.ts test/sessions-settings.test.ts test/app/user-agent.test.ts test/sessions.test.ts
npx eslint app/pages/app/settings app/components/auth/PasswordSettings.vue app/components/settings app/utils/userAgent.ts test
npx vue-tsc --noEmit
```

Do not change session, password, or agent-grant composables or any API call.
Report the first failing test, exact commands, and the `app.vue` diff for the
owner to merge.
