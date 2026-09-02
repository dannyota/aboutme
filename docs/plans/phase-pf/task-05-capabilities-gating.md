# Task 05 — `useCapabilities` and login/settings gating

**Acceptance:** AC-AUTH-017 (web clauses), AC-SEC-006 (settings requests only
enabled surfaces).

**Depends on:** T02 (generated `Capabilities` type), T04 (`AppChrome`).

**Owned paths:** `apps/web/app/composables/useCapabilities.ts`,
`apps/web/app/pages/login.vue`, `apps/web/app/pages/app/settings/sessions.vue`,
`apps/web/test/useCapabilities.test.ts`,
`apps/web/test/support/capabilities.ts`, and the existing tests listed in
step 5.

## Interfaces

- Consumes: `components['schemas']['Capabilities']` from
  `app/api/generated/openapi.ts` (T02).
- Produces:
  `useCapabilities(): { providerLogin: ComputedRef<boolean>, agentAccess: ComputedRef<boolean>, resolved: ComputedRef<boolean> }`
  and the test helper `registerCapabilities(flags)`. T06 relies on the login
  page showing provider links when the harness flag is true.

## Contract

Client-side only (`server: false`), `credentials: 'omit'`, `cache: 'no-store'`.
Any failure or malformed body yields both flags false. The login page renders
the divider and provider list only when `providerLogin` is true. The settings
page renders the provider block only when `providerLogin` is true and
`ConnectedAgents` only when `agentAccess` is true, so it never requests
`/api/v1/me/agents` when agent access is off.

## Steps

- [ ] **Step 1: Write the failing composable test**

Create `apps/web/test/support/capabilities.ts`:

```ts
import { registerEndpoint } from "@nuxt/test-utils/runtime";
import { setResponseStatus } from "h3";

export interface CapabilityFlags {
  providerLogin: boolean;
  agentAccess: boolean;
}

/** Registers GET /api/v1/capabilities; a null argument makes it fail with 500. */
export function registerCapabilities(
  flags: CapabilityFlags | null = { providerLogin: true, agentAccess: true },
): void {
  registerEndpoint("/api/v1/capabilities", (event) => {
    if (flags === null) {
      setResponseStatus(event, 500);
      return { error: { code: "internal", message: "unavailable" } };
    }
    return { data: flags };
  });
}
```

Create `apps/web/test/useCapabilities.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { mountSuspended } from "@nuxt/test-utils/runtime";
import { flushPromises } from "@vue/test-utils";
import { defineComponent, h } from "vue";
import { useCapabilities } from "../app/composables/useCapabilities";
import { registerCapabilities } from "./support/capabilities";

const Probe = defineComponent({
  setup() {
    const { providerLogin, agentAccess, resolved } = useCapabilities();
    return () =>
      h("div", {
        "data-provider": String(providerLogin.value),
        "data-agent": String(agentAccess.value),
        "data-resolved": String(resolved.value),
      });
  },
});

async function probe(): Promise<Record<string, string | undefined>> {
  const wrapper = await mountSuspended(Probe);
  await flushPromises();
  const el = wrapper.get("div");
  return {
    provider: el.attributes("data-provider"),
    agent: el.attributes("data-agent"),
    resolved: el.attributes("data-resolved"),
  };
}

describe("useCapabilities", () => {
  it("reflects both flags from the server", async () => {
    registerCapabilities({ providerLogin: true, agentAccess: false });
    expect(await probe()).toEqual({
      provider: "true",
      agent: "false",
      resolved: "true",
    });
  });

  it("treats a failed read as all false", async () => {
    registerCapabilities(null);
    expect(await probe()).toEqual({
      provider: "false",
      agent: "false",
      resolved: "true",
    });
  });

  it("treats a malformed body as all false", async () => {
    registerCapabilities({
      providerLogin: "yes" as unknown as boolean,
      agentAccess: 1 as unknown as boolean,
    });
    expect(await probe()).toEqual({
      provider: "false",
      agent: "false",
      resolved: "true",
    });
  });
});
```

`registerEndpoint` replaces a previous registration for the same path, so each
test registers its own variant before mounting.

- [ ] **Step 2: Run and watch it fail**

```sh
cd apps/web && npx vitest run test/useCapabilities.test.ts
```

Expected: cannot resolve `../app/composables/useCapabilities`.

- [ ] **Step 3: Implement `app/composables/useCapabilities.ts`**

```ts
import type { ComputedRef } from "vue";
import type { components } from "../api/generated/openapi";

export type Capabilities = components["schemas"]["Capabilities"];

interface CapabilitiesEnvelope {
  data: Capabilities;
}

export interface UseCapabilitiesReturn {
  providerLogin: ComputedRef<boolean>;
  agentAccess: ComputedRef<boolean>;
  resolved: ComputedRef<boolean>;
}

/**
 * `useCapabilities` — which optional surfaces this deployment enables, read
 * from `GET /api/v1/capabilities` in the browser only (Nuxt never fetches Go
 * during SSR). Anything but an exact boolean `true` is `false`, so a failed
 * or malformed read hides every optional surface.
 */
export function useCapabilities(): UseCapabilitiesReturn {
  const { data, status } = useFetch<CapabilitiesEnvelope>(
    "/api/v1/capabilities",
    { server: false, credentials: "omit", cache: "no-store" },
  );
  const providerLogin = computed(
    () => data.value?.data?.providerLogin === true,
  );
  const agentAccess = computed(() => data.value?.data?.agentAccess === true);
  const resolved = computed(
    () => status.value === "success" || status.value === "error",
  );
  return { providerLogin, agentAccess, resolved };
}
```

- [ ] **Step 4: Run to GREEN**

```sh
cd apps/web && npx vitest run test/useCapabilities.test.ts
```

Expected: three tests pass.

- [ ] **Step 5: Register capabilities in the existing page tests**

Each of these mounts a page that now reads capabilities; add
`import { registerCapabilities } from './support/capabilities';` and a top-level
`registerCapabilities();` (both true, preserving today's behavior):
`test/login.test.ts`, `test/sessions.test.ts`,
`test/sessions-csrf-gating.test.ts`, `test/sessions-nullable-fields.test.ts`,
`test/sessions-privileged-start-adversarial.test.ts`,
`test/sessions-password.test.ts`, `test/password-settings.test.ts`,
`test/connected-agents.test.ts`. Run `make web-test` and confirm it is still
green before changing any page.

- [ ] **Step 6: Write the failing login gating tests**

Append to `test/login.test.ts`:

```ts
describe("login.vue provider gating", () => {
  it("renders no provider link or divider when providerLogin is false", async () => {
    registerCapabilities({ providerLogin: false, agentAccess: false });
    const wrapper = await mountSuspended(LoginPage);
    await flushPromises();
    expect(wrapper.find(".login-providers").exists()).toBe(false);
    expect(wrapper.find(".auth-divider").exists()).toBe(false);
    expect(wrapper.find('a[href^="/api/v1/auth/"]').exists()).toBe(false);
    // The password form is unconditional.
    expect(wrapper.find("form.auth-form").exists()).toBe(true);
  });

  it("renders no provider link when the capabilities read fails", async () => {
    registerCapabilities(null);
    const wrapper = await mountSuspended(LoginPage);
    await flushPromises();
    expect(wrapper.find('a[href^="/api/v1/auth/"]').exists()).toBe(false);
  });

  it("renders the provider links after providerLogin resolves true", async () => {
    registerCapabilities({ providerLogin: true, agentAccess: false });
    const wrapper = await mountSuspended(LoginPage);
    await flushPromises();
    expect(wrapper.findAll('a[href^="/api/v1/auth/"]')).toHaveLength(3);
  });
});
```

- [ ] **Step 7: Run and watch them fail**

```sh
cd apps/web && npx vitest run test/login.test.ts
```

Expected: the first two new tests fail because the links are always rendered.

- [ ] **Step 8: Gate the login page**

In `login.vue`'s script, add
`import { useCapabilities } from '../composables/useCapabilities';` and
`const { providerLogin } = useCapabilities();`. Wrap the divider and list:

```vue
<template v-if="providerLogin">
  <div class="auth-divider">or</div>

  <ul class="login-providers">
    <!-- unchanged list body -->
  </ul>
</template>
```

Update the file's header comment: provider links render only when the
capabilities read reports `providerLogin`.

- [ ] **Step 9: Run the login tests to GREEN**

```sh
cd apps/web && npx vitest run test/login.test.ts
```

- [ ] **Step 10: Write the failing settings gating tests**

Append to `test/sessions.test.ts` (reuse its `meData` and `sessionsData`
registrations):

```ts
describe("sessions.vue capability gating", () => {
  it("hides the provider block when providerLogin is false", async () => {
    registerCapabilities({ providerLogin: false, agentAccess: true });
    const wrapper = await mountSuspended(SessionsPage);
    await flushPromises();
    expect(wrapper.text()).not.toContain("Add another sign-in provider");
  });

  it("hides Connected agents and never requests the grant list when agentAccess is false", async () => {
    let agentRequests = 0;
    registerEndpoint("/api/v1/me/agents", () => {
      agentRequests += 1;
      return { data: { grants: [] } };
    });
    registerCapabilities({ providerLogin: true, agentAccess: false });
    const wrapper = await mountSuspended(SessionsPage);
    await flushPromises();
    expect(wrapper.text()).not.toContain("Connected agents");
    expect(agentRequests).toBe(0);
    // Sessions and password remain.
    expect(wrapper.text()).toContain("Signed-in devices");
    expect(wrapper.text()).toContain("Password");
  });

  it("shows both blocks when both flags are true", async () => {
    registerEndpoint("/api/v1/me/agents", () => ({ data: { grants: [] } }));
    registerCapabilities({ providerLogin: true, agentAccess: true });
    const wrapper = await mountSuspended(SessionsPage);
    await flushPromises();
    expect(wrapper.text()).toContain("Add another sign-in provider");
    expect(wrapper.text()).toContain("Connected agents");
  });
});
```

- [ ] **Step 11: Run and watch them fail**

```sh
cd apps/web && npx vitest run test/sessions.test.ts
```

Expected: the first two new tests fail.

- [ ] **Step 12: Gate the settings page**

In `sessions.vue`'s script add
`import { useCapabilities } from '../../../composables/useCapabilities';` and
`const { providerLogin, agentAccess } = useCapabilities();`. In the template,
change the provider section's condition to
`v-if="providerLogin && unlinkedProviders.length"` and wrap the agents block:

```vue
<ConnectedAgents v-if="agentAccess" />
```

Leave the reauthentication prompt logic unchanged; provider-only accounts cannot
exist while the flag is off.

- [ ] **Step 13: Run the settings tests and the full web gate**

```sh
cd apps/web && npx vitest run test/sessions.test.ts test/connected-agents.test.ts
make web-lint web-typecheck web-test web-build
```

Expected: all pass.

- [ ] **Step 14: Confirm the 404 is gone**

With the native stack (agent access off), open
`http://localhost:20080/app/settings/sessions` signed in. The Connected agents
heading is absent and the browser console shows no request to
`/api/v1/me/agents`.

## Adversarial checklist

- `providerLogin: "true"` (a string) is false: only boolean `true` enables a
  surface.
- A 200 with `{}` or `{"data":null}` is false for both.
- The settings page never renders a provider start link while the read is
  pending; `resolved` is not needed for that because the flags start false.

## Handoff

Report RED and GREEN outputs for steps 2, 7, 11, and 13, and the list of tests
you touched in step 5. Suggested commit:
`feat(web): gate provider and agent surfaces on capabilities`.
