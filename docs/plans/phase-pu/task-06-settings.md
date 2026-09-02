# Task 06 — Settings, password, connected agents

**Acceptance:** AC-UI-002, AC-UI-005, AC-UI-006.

**Depends on:** T03.

**Owned paths:** T06 paths in `file-structure.md`.

## Contract

- `pages/app/settings/sessions.vue` renders `PageHeader` (`Settings`,
  description `Signed-in devices, your password, and connected agents.`) and
  three `Card` sections in this order: `Signed-in devices`, `Password`,
  `Connected agents` (the last only when `agentAccess`). The provider block
  (`Add another sign-in provider`, `Link {provider}`) stays inside the devices
  card and only when `providerLogin`.
- Devices are a `Table` with columns `Device`, `Last seen`, and actions. The
  current row shows a `Badge` `This device` and the `Log out` button; other rows
  show `Revoke` (`data-testid="revoke-button"`). `Log out everywhere`
  (`data-testid="revoke-all-button"`) sits in the card footer as
  `Button variant="outline"`. Every action stays disabled without a CSRF token.
  The user-agent string renders as text in `text-muted-foreground`.
- `revoke-error`, `link-error`, and `reauth-prompt` render through
  `StatusBanner kind="error"`; the reauth prompt keeps its inline
  `Sign in again with {provider}` button.
- `PasswordSettings.vue` keeps its script and renders: the status line
  (`data-testid="password-status"`), banners (`password-success` as
  `kind="success"`, `password-error` as `kind="error"` with `focusOnMount`), the
  idle action button, and the two forms on `FormField` + `PasswordField` with
  `Button` submit and `Button variant="ghost"` cancel. Every `data-testid` in
  the retained hooks list stays on the same control.
- `ConnectedAgents.vue` keeps its script except the confirmation: each grant is
  a bordered row (`data-testid="agent-row"`) with the client name as an `h3`,
  scopes as `Badge variant="secondary"` items, the two `time` lines, and a
  `Revoke` button (`data-testid="agent-revoke"`). The confirmation is
  `ConfirmDialog` (`title="Revoke access"`,
  `description="Revoke this connected agent's access?"`,
  `confirmLabel="Revoke access"`, `destructive`, `:busy="revokePending"`).
  Loading is `LoadingState` with `testid="agents-loading"`; the error is
  `StatusBanner kind="error" testid="agents-error"` with the `Retry` button
  (`data-testid="agents-retry"`); the empty state is `EmptyState`.

## Hook changes

- The revoke confirmation is an `alertdialog`, not a `dialog` with a `form`; the
  confirm button is `[data-action="agent-revoke-confirm"]` and cancel
  `[data-action="agent-revoke-cancel"]`.
- The provider list uses `Button variant="outline"` items; texts unchanged.

## Strings held

`Signed-in devices`, `This device`, `Log out`, `Revoke`, `Log out everywhere`,
`Add another sign-in provider`, `Link {provider}`,
`Sign in again with {provider}`, both reauth messages, the three link error
messages, `Could not revoke that session. Try again.`,
`Could not log out everywhere. Try again.`, `Connected agents`,
`Loading connected agents…`, `Connected agents are unavailable. Try again.`,
`No connected agents. Agents connect through MCP after you approve access.`,
`Read resumes`, `Write resumes`, `Created`, `Last used`, `Never`,
`Revoke access`, `Cancel`, `Password`, `You have a password.`,
`No password set.`, `Change password`, `Add a password`, `Save password`,
`Continue`, `Continue with {Provider}`,
`Sign in again with your provider to continue.`

## TDD cycle

- [ ] **RED.** In `test/connected-agents.test.ts` replace the confirmation cases
      with:

  ```ts
  it("revokes through the confirm dialog and returns focus", async () => {
    grantsResponse = [grant("g1", "Agent One")];
    const wrapper = await mountSuspended(ConnectedAgents, {
      attachTo: document.body,
    });
    await flushPromises();
    const trigger = wrapper.get('[data-testid="agent-revoke"]');
    (trigger.element as HTMLButtonElement).focus();
    await trigger.trigger("click");
    await nextTick();
    const dialog = document.body.querySelector('[role="alertdialog"]')!;
    expect(dialog.textContent).toContain("Revoke access");
    document.body
      .querySelector<HTMLButtonElement>('[data-action="agent-revoke-confirm"]')!
      .click();
    await flushPromises();
    expect(revokedIds).toEqual(["g1"]);
    expect(document.body.querySelector('[role="alertdialog"]')).toBeNull();
    wrapper.unmount();
  });
  ```

  (`grant`, `grantsResponse`, and `revokedIds` are the file's existing fixtures
  and endpoint spies; keep their names.) In `test/sessions.test.ts` and the four
  `sessions-*.test.ts` files keep every assertion and change structural
  selectors: session rows are `tr` elements found by
  `[data-testid="session-row-{id}"]`; buttons by `data-testid` or text. In
  `test/password-settings.test.ts` keep every `data-testid` assertion and
  replace `input[autocomplete="..."]` selectors with `#password-new`,
  `#password-new-confirm`, and `#password-current`. Add to
  `test/sessions.test.ts`:

  ```ts
  it("renders devices as a table with a current-device badge", async () => {
    const wrapper = await mountSessions();
    const current = wrapper.get('[data-testid="session-row-current"]');
    expect(current.element.tagName).toBe("TR");
    expect(current.text()).toContain("This device");
    expect(current.find('[data-testid="revoke-button"]').exists()).toBe(false);
    expect(wrapper.find('button:not([data-slot="button"])').exists()).toBe(
      false,
    );
  });
  ```

  using the file's existing mount helper and fixture ids.

- [ ] Run and watch the changed cases fail:

  ```sh
  cd apps/web && npx vitest run test/sessions.test.ts test/sessions-csrf-gating.test.ts test/sessions-nullable-fields.test.ts test/sessions-password.test.ts test/sessions-privileged-start-adversarial.test.ts test/password-settings.test.ts test/connected-agents.test.ts
  ```

- [ ] **Sessions page template** (script unchanged):

  ```vue
  <template>
    <main class="mx-auto flex w-full max-w-4xl flex-col gap-6 px-5 py-10">
      <PageHeader
        title="Settings"
        description="Signed-in devices, your password, and connected agents."
      />
      <StatusBanner v-if="revokeError" kind="error" testid="revoke-error">
        {{ revokeError }}
      </StatusBanner>
      <StatusBanner v-if="linkErrorMessage" kind="error" testid="link-error">
        {{ linkErrorMessage }}
      </StatusBanner>
      <StatusBanner
        v-if="providerLogin && reauthRequired && reauthProvider"
        kind="error"
        testid="reauth-prompt"
      >
        {{ reauthMessage }}
        <Button
          class="mt-2"
          :disabled="!csrfToken || startPending"
          size="sm"
          variant="outline"
          @click="startOAuth(reauthProvider, 'reauth')"
        >
          Sign in again with {{ reauthProvider }}
        </Button>
      </StatusBanner>

      <Card>
        <CardHeader>
          <CardTitle as="h2">Signed-in devices</CardTitle>
          <CardDescription
            >Every browser with an active session for your
            account.</CardDescription
          >
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Device</TableHead>
                <TableHead class="w-48">Last seen</TableHead>
                <TableHead class="w-40 text-right"
                  ><span class="sr-only">Actions</span></TableHead
                >
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow
                v-for="session in sessions"
                :key="session.id"
                :data-testid="`session-row-${session.id}`"
              >
                <TableCell>
                  <span class="text-muted-foreground">{{
                    session.ua ?? "Unknown device"
                  }}</span>
                  <Badge v-if="session.current" class="ml-2" variant="secondary"
                    >This device</Badge
                  >
                </TableCell>
                <TableCell class="text-muted-foreground"
                  >Last seen {{ session.lastSeenAt }}</TableCell
                >
                <TableCell class="text-right">
                  <Button
                    v-if="session.current"
                    :disabled="!csrfToken"
                    size="sm"
                    variant="ghost"
                    @click="logout"
                    >Log out</Button
                  >
                  <Button
                    v-else
                    data-testid="revoke-button"
                    :disabled="!csrfToken"
                    size="sm"
                    variant="ghost"
                    @click="revokeSession(session.id)"
                    >Revoke</Button
                  >
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
          <div v-if="providerLogin && unlinkedProviders.length" class="mt-4">
            <Button
              v-if="!showAddProvider"
              data-testid="add-provider-button"
              size="sm"
              variant="outline"
              @click="openAddProvider"
            >
              Add another sign-in provider
            </Button>
            <ul v-else class="flex flex-wrap gap-2">
              <li v-for="provider in unlinkedProviders" :key="provider">
                <Button
                  :disabled="!csrfToken || startPending"
                  size="sm"
                  variant="outline"
                  @click="startOAuth(provider, 'link')"
                >
                  Link {{ provider }}
                </Button>
              </li>
            </ul>
          </div>
        </CardContent>
        <CardFooter>
          <Button
            data-testid="revoke-all-button"
            :disabled="!csrfToken"
            variant="outline"
            @click="revokeAll"
          >
            Log out everywhere
          </Button>
        </CardFooter>
      </Card>

      <PasswordSettings
        :has-password="user?.hasPassword ?? false"
        :providers="passwordProviders"
        @updated="onPasswordUpdated"
      />
      <ConnectedAgents v-if="agentAccess" />
    </main>
  </template>
  ```

  `PasswordSettings` and `ConnectedAgents` each render their own `Card` with an
  `h2` `CardTitle`. Keep the existing `Last seen {{ session.lastSeenAt }}` text
  so the sessions tests match.

- [ ] Implement `PasswordSettings.vue` and `ConnectedAgents.vue` templates per
      the contract; the agents confirmation replaces `selected`, `confirmation`,
      and `returnFocus` handling with `ConfirmDialog :open="selected !== null"`
      and `@confirm="confirmRevoke" @cancel="closeConfirmation"` (drop the
      manual `nextTick` focus code; the dialog returns focus).

- [ ] GREEN:

  ```sh
  cd apps/web && npx vitest run test/sessions.test.ts test/sessions-csrf-gating.test.ts test/sessions-nullable-fields.test.ts test/sessions-password.test.ts test/sessions-privileged-start-adversarial.test.ts test/password-settings.test.ts test/connected-agents.test.ts
  make web-lint web-typecheck
  ```

## Adversarial checklist

- Author: the T06 cases in `adversarial-coverage.md`.

## Handoff

Report RED and GREEN outputs and every selector change made in the seven test
files. Suggested commit: `feat(web): rebuild settings on the shared components`.
