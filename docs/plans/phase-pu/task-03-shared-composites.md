# Task 03 — Shared composites

**Acceptance:** AC-UI-002, AC-UI-003.

**Depends on:** T01 (primitives, `cn`, tokens).

**Owned paths:** T03 paths in `file-structure.md`.

## Contract

Implement every composite in [component-contracts.md](component-contracts.md)
under `app/components/app/`, each with `<script setup lang="ts">`, explicit
primitive imports, no `<style>` block, and a `class` pass-through. Replace
`AppChrome.vue` and `AccountControl.vue` with `AppShell.vue` and
`AccountMenu.vue`; rebuild `ThemeToggle.vue` in place. Reduce `fieldIntent.ts`
to `set | unset`. Retarget the three shell tests.

**Interfaces:**

- Consumes: `@/components/ui/*` from T01; `useAuth()`, `useTheme()`.
- Produces: the contracts file, verbatim. Later tasks import
  `@/components/app/<Name>.vue`.

## Hook changes

- `.account-control` (class) becomes `[data-testid="account-menu"]`; the account
  name is the trigger's text; `Log out` moves into the menu.
- `AppShell` root gains `data-testid="app-shell"`.

## TDD cycle

- [ ] **fieldIntent.** Change `app/components/editor/forms/fieldIntent.ts` to:

  ```ts
  export type FieldIntent<T> =
    { readonly kind: "set"; readonly value: T } | { readonly kind: "unset" };
  ```

  Run `make web-typecheck`; every `clear` producer it reports belongs to a later
  task (T08, T09) and is left alone. Record the list in the report.

- [ ] **FormField RED.** Create `test/app/form-field.test.ts`:

  ```ts
  import { mount } from "@vue/test-utils";
  import { describe, expect, it } from "vitest";

  import FormField from "../../app/components/app/FormField.vue";

  const slotInput = `<template #default="{ id, describedBy, invalid }">
    <input :id="id" :aria-describedby="describedBy" :aria-invalid="invalid">
  </template>`;

  describe("FormField", () => {
    it("links label, hint, and error to the control", () => {
      const wrapper = mount(FormField, {
        props: {
          label: "Email",
          id: "email",
          hint: "Work address",
          error: "Required",
          name: "email",
        },
        slots: { default: slotInput },
      });
      const input = wrapper.get("input");
      expect(wrapper.get("label").attributes("for")).toBe("email");
      expect(input.attributes("aria-describedby")).toBe(
        "email-hint email-error",
      );
      expect(input.attributes("aria-invalid")).toBe("true");
      expect(wrapper.get('[role="alert"]').attributes("id")).toBe(
        "email-error",
      );
      expect(wrapper.get('[role="alert"]').attributes("data-error-for")).toBe(
        "email",
      );
      expect(wrapper.attributes("data-field")).toBe("email");
    });

    it("omits describedby and invalid without hint or error", () => {
      const wrapper = mount(FormField, {
        props: { label: "Email" },
        slots: { default: slotInput },
      });
      const input = wrapper.get("input");
      expect(input.attributes("id")).toMatch(/^field-/);
      expect(input.attributes("aria-describedby")).toBeUndefined();
      expect(input.attributes("aria-invalid")).toBeUndefined();
      expect(wrapper.find('[role="alert"]').exists()).toBe(false);
    });
  });
  ```

- [ ] **TextField RED.** Create `test/app/text-field.test.ts`:

  ```ts
  import { mount } from "@vue/test-utils";
  import { describe, expect, it } from "vitest";

  import TextField from "../../app/components/app/TextField.vue";

  function field(modelValue?: string) {
    return mount(TextField, { props: { label: "Name", modelValue } });
  }
  const input = (wrapper: ReturnType<typeof field>) =>
    wrapper.get("[data-field-input]");

  describe("TextField commit rule (decisions U4)", () => {
    it("sets a non-empty changed value on blur", async () => {
      const wrapper = field(undefined);
      await input(wrapper).setValue("Ada");
      await input(wrapper).trigger("blur");
      expect(wrapper.emitted("intent")).toEqual([
        [{ kind: "set", value: "Ada" }],
      ]);
    });

    it("sets on Enter in a single-line field", async () => {
      const wrapper = field("Ada");
      await input(wrapper).setValue("Ada Lovelace");
      await input(wrapper).trigger("keydown", { key: "Enter" });
      expect(wrapper.emitted("intent")).toEqual([
        [{ kind: "set", value: "Ada Lovelace" }],
      ]);
    });

    it("unsets when a defined value is emptied", async () => {
      const wrapper = field("Ada");
      await input(wrapper).setValue("");
      await input(wrapper).trigger("blur");
      expect(wrapper.emitted("intent")).toEqual([[{ kind: "unset" }]]);
    });

    it("emits nothing for an empty undefined field", async () => {
      const wrapper = field(undefined);
      await input(wrapper).trigger("blur");
      expect(wrapper.emitted("intent")).toBeUndefined();
    });

    it("emits nothing when the value is unchanged", async () => {
      const wrapper = field("Ada");
      await input(wrapper).setValue("Ad");
      await input(wrapper).setValue("Ada");
      await input(wrapper).trigger("blur");
      expect(wrapper.emitted("intent")).toBeUndefined();
    });

    it("reverts on Escape and then emits nothing on blur", async () => {
      const wrapper = field("Ada");
      await input(wrapper).setValue("Grace");
      await input(wrapper).trigger("keydown", { key: "Escape" });
      expect((input(wrapper).element as HTMLInputElement).value).toBe("Ada");
      await input(wrapper).trigger("blur");
      expect(wrapper.emitted("intent")).toBeUndefined();
    });

    it("follows an external model change while clean", async () => {
      const wrapper = field("Ada");
      await wrapper.setProps({ modelValue: "Grace" });
      expect((input(wrapper).element as HTMLInputElement).value).toBe("Grace");
    });

    it("keeps the draft on an external model change while dirty", async () => {
      const wrapper = field("Ada");
      await input(wrapper).setValue("Typing");
      await wrapper.setProps({ modelValue: "Grace" });
      expect((input(wrapper).element as HTMLInputElement).value).toBe("Typing");
    });

    it("does not commit on Enter in a multiline field", async () => {
      const wrapper = mount(TextField, {
        props: { label: "Summary", modelValue: "a", multiline: true },
      });
      await wrapper.get("[data-field-input]").setValue("a\nb");
      await wrapper
        .get("[data-field-input]")
        .trigger("keydown", { key: "Enter" });
      expect(wrapper.emitted("intent")).toBeUndefined();
    });

    it("wires label, hint, and error through FormField", () => {
      const wrapper = mount(TextField, {
        props: {
          label: "Name",
          id: "n",
          hint: "h",
          error: "e",
          name: "fullName",
        },
      });
      expect(wrapper.get("label").attributes("for")).toBe("n");
      expect(
        wrapper.get("[data-field-input]").attributes("aria-describedby"),
      ).toBe("n-hint n-error");
      expect(wrapper.attributes("data-field")).toBe("fullName");
    });
  });
  ```

- [ ] **ConfirmDialog RED.** Create `test/app/confirm-dialog.test.ts`:

  ```ts
  import { mount } from "@vue/test-utils";
  import { nextTick } from "vue";
  import { describe, expect, it } from "vitest";

  import ConfirmDialog from "../../app/components/app/ConfirmDialog.vue";

  function open(props: Record<string, unknown> = {}) {
    const trigger = document.createElement("button");
    document.body.append(trigger);
    trigger.focus();
    const wrapper = mount(ConfirmDialog, {
      attachTo: document.body,
      props: {
        open: true,
        title: "Delete resume",
        description: "This cannot be undone.",
        confirmLabel: "Delete",
        confirmAction: "confirm-delete",
        cancelAction: "cancel-delete",
        ...props,
      },
    });
    return { wrapper, trigger };
  }
  const body = () => document.body;

  describe("ConfirmDialog", () => {
    it("renders an alert dialog with title and description", async () => {
      const { wrapper } = open();
      await nextTick();
      const dialog = body().querySelector('[role="alertdialog"]')!;
      expect(dialog.getAttribute("aria-labelledby")).toBeTruthy();
      expect(dialog.textContent).toContain("Delete resume");
      expect(dialog.textContent).toContain("This cannot be undone.");
      wrapper.unmount();
    });

    it("gates confirm on the exact typed text", async () => {
      const { wrapper } = open({
        confirmText: "My resume",
        confirmInputLabel: "Current title",
      });
      await nextTick();
      const confirm = body().querySelector<HTMLButtonElement>(
        '[data-action="confirm-delete"]',
      )!;
      const input = body().querySelector<HTMLInputElement>("input")!;
      expect(confirm.disabled).toBe(true);
      input.value = "my resume";
      input.dispatchEvent(new Event("input"));
      await nextTick();
      expect(confirm.disabled).toBe(true);
      input.value = "My resume";
      input.dispatchEvent(new Event("input"));
      await nextTick();
      expect(confirm.disabled).toBe(false);
      confirm.click();
      expect(wrapper.emitted("confirm")).toHaveLength(1);
      wrapper.unmount();
    });

    it("focuses cancel when destructive and returns focus to the opener", async () => {
      const { wrapper, trigger } = open({ destructive: true });
      await nextTick();
      await nextTick();
      expect(document.activeElement).toBe(
        body().querySelector('[data-action="cancel-delete"]'),
      );
      await wrapper.setProps({ open: false });
      await nextTick();
      await nextTick();
      expect(document.activeElement).toBe(trigger);
      wrapper.unmount();
      trigger.remove();
    });

    it("ignores Escape while busy", async () => {
      const { wrapper } = open({ busy: true });
      await nextTick();
      body()
        .querySelector('[role="alertdialog"]')!
        .dispatchEvent(
          new KeyboardEvent("keydown", { key: "Escape", bubbles: true }),
        );
      await nextTick();
      expect(wrapper.emitted("cancel")).toBeUndefined();
      wrapper.unmount();
    });

    it("renders a hostile title as text", async () => {
      const { wrapper } = open({ title: "<img src=x onerror=alert(1)>" });
      await nextTick();
      expect(body().querySelector('[role="alertdialog"] img')).toBeNull();
      expect(
        body().querySelector('[role="alertdialog"]')!.textContent,
      ).toContain("<img");
      wrapper.unmount();
    });
  });
  ```

- [ ] **StatusBanner RED.** Create `test/app/status-banner.test.ts`:

  ```ts
  import { mount } from "@vue/test-utils";
  import { nextTick } from "vue";
  import { describe, expect, it } from "vitest";

  import StatusBanner from "../../app/components/app/StatusBanner.vue";

  describe("StatusBanner", () => {
    it.each([
      ["error", "alert"],
      ["success", "status"],
      ["info", "status"],
    ] as const)("%s renders role %s", (kind, role) => {
      const wrapper = mount(StatusBanner, {
        props: { kind, testid: "x" },
        slots: { default: "Saved." },
      });
      expect(wrapper.attributes("role")).toBe(role);
      expect(wrapper.attributes("data-testid")).toBe("x");
      expect(wrapper.text()).toContain("Saved.");
    });

    it("focuses itself on mount when asked", async () => {
      const wrapper = mount(StatusBanner, {
        attachTo: document.body,
        props: { kind: "error", focusOnMount: true },
        slots: { default: "Fix this." },
      });
      await nextTick();
      expect(document.activeElement).toBe(wrapper.element);
      wrapper.unmount();
    });
  });
  ```

- [ ] **FormDialog RED.** Create `test/app/form-dialog.test.ts` with three cases
      mirroring the confirm dialog file: `submit` is emitted once from the
      form's `submit` event and not at all while `busy`; `cancel` is emitted
      from `update:open(false)` unless `busy`; the first focusable control in
      the default slot (`<input id="title">`) has focus after two ticks.

- [ ] **AppShell RED.** Move `test/app-chrome.test.ts` to
      `test/app/app-shell.test.ts`, import `AppShell`, and replace the two
      `.account-control` assertions with
      `wrapper.find('[data-testid="account-menu"]').exists()` and
      `wrapper.get('[data-testid="account-menu"]').text()`. Retarget
      `test/logout-state.test.ts` to `AppShell` the same way. Keep every other
      assertion (link texts and hrefs, theme toggle `aria-label`).

- [ ] Run the new suites and watch them fail on missing components:

  ```sh
  cd apps/web && npx vitest run test/app test/logout-state.test.ts test/editor/theme.test.ts
  ```

- [ ] **Implement FormField** (`app/components/app/FormField.vue`):

  ```vue
  <script setup lang="ts">
  import { computed, useId } from "vue";

  import { Label } from "@/components/ui/label";
  import { cn } from "@/lib/utils";

  const props = defineProps<{
    readonly label: string;
    readonly id?: string;
    readonly name?: string;
    readonly hint?: string;
    readonly error?: string;
    readonly required?: boolean;
    readonly class?: string;
  }>();

  const generated = useId();
  const id = computed(() => props.id ?? `field-${generated}`);
  const hintId = computed(() => (props.hint ? `${id.value}-hint` : undefined));
  const errorId = computed(() =>
    props.error ? `${id.value}-error` : undefined,
  );
  const describedBy = computed(() => {
    const ids = [hintId.value, errorId.value].filter(
      (value): value is string => value !== undefined,
    );
    return ids.length === 0 ? undefined : ids.join(" ");
  });
  const invalid = computed(() => (props.error ? (true as const) : undefined));
  </script>

  <template>
    <div :class="cn('grid gap-1.5', props.class)" :data-field="name">
      <Label :for="id" class="text-sm font-medium">
        {{ label }}
      </Label>
      <slot :id="id" :described-by="describedBy" :invalid="invalid" />
      <p v-if="hint" :id="hintId" class="text-xs text-muted-foreground">
        {{ hint }}
      </p>
      <p
        v-if="error"
        :id="errorId"
        role="alert"
        :data-error-for="name"
        class="text-xs text-destructive"
      >
        {{ error }}
      </p>
    </div>
  </template>
  ```

- [ ] **Implement TextField** (`app/components/app/TextField.vue`):

  ```vue
  <script setup lang="ts">
  import { ref, watch } from "vue";

  import type { FieldIntent } from "@/components/editor/forms/fieldIntent";
  import { Input } from "@/components/ui/input";
  import { Textarea } from "@/components/ui/textarea";
  import FormField from "./FormField.vue";

  const props = withDefaults(
    defineProps<{
      readonly label: string;
      readonly modelValue?: string;
      readonly id?: string;
      readonly name?: string;
      readonly type?: "text" | "email" | "url";
      readonly multiline?: boolean;
      readonly rows?: number;
      readonly autocomplete?: string;
      readonly inputmode?: string;
      readonly placeholder?: string;
      readonly hint?: string;
      readonly error?: string;
      readonly required?: boolean;
      readonly disabled?: boolean;
      readonly class?: string;
    }>(),
    { type: "text", rows: 3 },
  );
  const emit = defineEmits<{ intent: [intent: FieldIntent<string>] }>();

  const draft = ref(props.modelValue ?? "");
  const dirty = ref(false);
  const control = ref<{ $el?: HTMLElement } | null>(null);

  watch(
    () => props.modelValue,
    (next) => {
      if (!dirty.value) draft.value = next ?? "";
    },
  );

  function onInput(value: string | number): void {
    draft.value = String(value);
    dirty.value = draft.value !== (props.modelValue ?? "");
  }

  function commit(): void {
    if (!dirty.value) return;
    dirty.value = false;
    if (draft.value === "") {
      if (props.modelValue !== undefined) emit("intent", { kind: "unset" });
      return;
    }
    if (draft.value !== props.modelValue) {
      emit("intent", { kind: "set", value: draft.value });
    }
  }

  function onKeydown(event: KeyboardEvent): void {
    if (event.key === "Escape") {
      event.preventDefault();
      draft.value = props.modelValue ?? "";
      dirty.value = false;
      return;
    }
    if (event.key === "Enter" && !props.multiline) {
      event.preventDefault();
      commit();
    }
  }

  defineExpose({ focus: (): void => control.value?.$el?.focus() });
  </script>

  <template>
    <FormField
      v-slot="{ id: fieldId, describedBy, invalid }"
      :id="id"
      :class="props.class"
      :error="error"
      :hint="hint"
      :label="label"
      :name="name"
      :required="required"
    >
      <component
        :is="multiline ? Textarea : Input"
        :id="fieldId"
        ref="control"
        :aria-describedby="describedBy"
        :aria-invalid="invalid"
        :autocomplete="autocomplete"
        data-field-input
        :disabled="disabled"
        :inputmode="inputmode"
        :model-value="draft"
        :placeholder="placeholder"
        :required="required"
        :rows="multiline ? rows : undefined"
        :type="multiline ? undefined : type"
        @blur="commit"
        @keydown="onKeydown"
        @update:model-value="onInput"
      />
    </FormField>
  </template>
  ```

- [ ] **Implement ConfirmDialog** (`app/components/app/ConfirmDialog.vue`):

  ```vue
  <script setup lang="ts">
  import { computed, ref, useId, watch } from "vue";

  import {
    AlertDialog,
    AlertDialogContent,
    AlertDialogDescription,
    AlertDialogFooter,
    AlertDialogHeader,
    AlertDialogTitle,
  } from "@/components/ui/alert-dialog";
  import { Button } from "@/components/ui/button";
  import { Input } from "@/components/ui/input";
  import { Label } from "@/components/ui/label";

  const props = withDefaults(
    defineProps<{
      readonly open: boolean;
      readonly title: string;
      readonly description: string;
      readonly confirmLabel: string;
      readonly cancelLabel?: string;
      readonly destructive?: boolean;
      readonly busy?: boolean;
      readonly confirmText?: string;
      readonly confirmInputLabel?: string;
      readonly confirmAction?: string;
      readonly cancelAction?: string;
    }>(),
    { cancelLabel: "Cancel" },
  );
  const emit = defineEmits<{ confirm: []; cancel: [] }>();

  const typed = ref("");
  const confirmId = `confirm-${useId()}`;
  const confirmButton = ref<{ $el?: HTMLElement } | null>(null);
  const cancelButton = ref<{ $el?: HTMLElement } | null>(null);
  const inputLabel = computed(
    () =>
      props.confirmInputLabel ?? `Type ${props.confirmText ?? ""} to confirm`,
  );
  const canConfirm = computed(
    () =>
      !props.busy &&
      (props.confirmText === undefined || typed.value === props.confirmText),
  );

  watch(
    () => props.open,
    (open) => {
      if (!open) typed.value = "";
    },
  );

  function onOpenChange(open: boolean): void {
    if (!open && !props.busy) emit("cancel");
  }

  function onOpenAutoFocus(event: Event): void {
    event.preventDefault();
    const target = props.destructive ? cancelButton.value : confirmButton.value;
    target?.$el?.focus();
  }

  function onEscape(event: Event): void {
    if (props.busy) event.preventDefault();
  }
  </script>

  <template>
    <AlertDialog :open="open" @update:open="onOpenChange">
      <AlertDialogContent
        :aria-busy="busy || undefined"
        @escape-key-down="onEscape"
        @open-auto-focus="onOpenAutoFocus"
      >
        <AlertDialogHeader>
          <AlertDialogTitle>{{ title }}</AlertDialogTitle>
          <AlertDialogDescription>{{ description }}</AlertDialogDescription>
        </AlertDialogHeader>
        <div v-if="confirmText !== undefined" class="grid gap-1.5">
          <Label :for="confirmId">{{ inputLabel }}</Label>
          <Input
            :id="confirmId"
            v-model="typed"
            autocomplete="off"
            :disabled="busy"
          />
        </div>
        <AlertDialogFooter>
          <Button
            ref="cancelButton"
            :data-action="cancelAction"
            :disabled="busy"
            type="button"
            variant="outline"
            @click="emit('cancel')"
          >
            {{ cancelLabel }}
          </Button>
          <Button
            ref="confirmButton"
            :data-action="confirmAction"
            :disabled="!canConfirm"
            type="button"
            :variant="destructive ? 'destructive' : 'default'"
            @click="emit('confirm')"
          >
            {{ confirmLabel }}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </template>
  ```

- [ ] **Implement FormDialog** (`app/components/app/FormDialog.vue`): the same
      shape on `Dialog`/`DialogContent`/`DialogHeader`/`DialogTitle`/
      `DialogDescription`/`DialogFooter`, wrapping the default slot in
      `<form novalidate @submit.prevent="onSubmit">` where `onSubmit` emits
      `submit` unless `busy`; a `footer` slot replaces the two buttons; the
      cancel button is `type="button"` and the submit button `type="submit"`
      with `:disabled="busy || submitDisabled"`; `@open-auto-focus` focuses the
      first element matching `input, select, textarea, button` inside the form.

- [ ] **Implement StatusBanner** (`app/components/app/StatusBanner.vue`):

  ```vue
  <script setup lang="ts">
  import { CheckCircle2, CircleAlert, Info } from "@lucide/vue";
  import { computed, onMounted, ref } from "vue";

  import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
  import { cn } from "@/lib/utils";

  const props = defineProps<{
    readonly kind: "info" | "success" | "error";
    readonly title?: string;
    readonly testid?: string;
    readonly focusOnMount?: boolean;
    readonly class?: string;
  }>();

  const root = ref<{ $el?: HTMLElement } | null>(null);
  const icon = computed(
    () =>
      ({
        info: Info,
        success: CheckCircle2,
        error: CircleAlert,
      })[props.kind],
  );

  onMounted(() => {
    if (props.focusOnMount) root.value?.$el?.focus();
  });

  defineExpose({ focus: (): void => root.value?.$el?.focus() });
  </script>

  <template>
    <Alert
      ref="root"
      :class="
        cn(kind === 'success' && 'border-positive text-positive', props.class)
      "
      :data-testid="testid"
      :role="kind === 'error' ? 'alert' : 'status'"
      tabindex="-1"
      :variant="kind === 'error' ? 'destructive' : 'default'"
    >
      <component :is="icon" aria-hidden="true" class="size-4" />
      <AlertTitle v-if="title">{{ title }}</AlertTitle>
      <AlertDescription><slot /></AlertDescription>
    </Alert>
  </template>
  ```

- [ ] **Implement AppShell, AccountMenu, ThemeToggle.** `AppShell.vue`:

  ```vue
  <script setup lang="ts">
  import { computed } from "vue";

  import { buttonVariants } from "@/components/ui/button";
  import AccountMenu from "./AccountMenu.vue";
  import ThemeToggle from "./ThemeToggle.vue";

  // Until /me resolves the shell is the signed-out variant (design: web,
  // "Application surfaces").
  const { authState } = useAuth();
  const route = useRoute();
  const signedIn = computed(() => authState.value === "authenticated");
  const links = [
    { to: "/app/resumes", label: "Resumes" },
    { to: "/app/settings/sessions", label: "Settings" },
  ] as const;
  const linkClass =
    "rounded-md px-2.5 py-1.5 text-sm text-muted-foreground " +
    "transition-colors hover:bg-accent hover:text-accent-foreground " +
    "aria-[current=page]:bg-accent aria-[current=page]:text-accent-foreground";
  </script>

  <template>
    <header
      class="flex min-h-14 items-center gap-4 border-b border-border bg-card px-[max(1rem,calc((100vw-76rem)/2))]"
      data-testid="app-shell"
    >
      <NuxtLink class="text-[0.925rem] font-bold tracking-tight" to="/">
        aboutme
      </NuxtLink>
      <nav
        v-if="signedIn"
        aria-label="Primary navigation"
        class="flex flex-1 items-center gap-1"
      >
        <NuxtLink
          v-for="link in links"
          :key="link.to"
          :aria-current="route.path.startsWith(link.to) ? 'page' : undefined"
          :class="linkClass"
          :to="link.to"
        >
          {{ link.label }}
        </NuxtLink>
      </nav>
      <div v-if="signedIn" class="ml-auto flex items-center gap-2">
        <AccountMenu />
        <ThemeToggle />
      </div>
      <div v-else class="ml-auto flex items-center gap-2">
        <NuxtLink
          :class="buttonVariants({ variant: 'ghost', size: 'sm' })"
          to="/login"
        >
          Sign in
        </NuxtLink>
        <NuxtLink
          :class="buttonVariants({ variant: 'secondary', size: 'sm' })"
          to="/register"
        >
          Create account
        </NuxtLink>
        <ThemeToggle />
      </div>
    </header>
  </template>
  ```

  `AccountMenu.vue`:

  ```vue
  <script setup lang="ts">
  import { LogOut, Settings2 } from "@lucide/vue";
  import { computed } from "vue";

  import { Avatar, AvatarFallback } from "@/components/ui/avatar";
  import { Button } from "@/components/ui/button";
  import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
  } from "@/components/ui/dropdown-menu";

  const { user, logout } = useAuth();
  const accountName = computed(
    () => user.value?.name?.trim() || user.value?.email || "Account",
  );
  const initials = computed(() =>
    accountName.value
      .split(/\s+/)
      .slice(0, 2)
      .map((part) => part[0]?.toUpperCase() ?? "")
      .join(""),
  );
  </script>

  <template>
    <DropdownMenu>
      <DropdownMenuTrigger as-child>
        <Button
          :aria-label="`Account settings for ${accountName}`"
          class="max-w-56 gap-2 pl-1"
          data-testid="account-menu"
          size="sm"
          variant="outline"
        >
          <Avatar class="size-7">
            <AvatarFallback class="bg-secondary text-xs text-positive">
              {{ initials }}
            </AvatarFallback>
          </Avatar>
          <span class="truncate">{{ accountName }}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem
          data-testid="account-menu-settings"
          @select="navigateTo('/app/settings/sessions')"
        >
          <Settings2 aria-hidden="true" />
          Settings
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem data-testid="account-menu-logout" @select="logout()">
          <LogOut aria-hidden="true" />
          Log out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  </template>
  ```

  `ThemeToggle.vue` keeps its script and renders
  `<Button variant="ghost" size="sm" :aria-label="..." @click="toggleTheme">`
  with the icon and `<span class="hidden md:inline">` around the text.

- [ ] **Implement the remaining composites** per the contracts: `PageHeader.vue`
      (flex row, `h1` classes `text-2xl font-semibold tracking-tight`, `actions`
      slot right-aligned), `SelectField.vue` (`FormField` around `NativeSelect`;
      if the generated `NativeSelect` does not forward `id` to its `<select>`,
      wrap the primitive's `<select>` classes directly in this file and record
      it in the report), `CheckboxField.vue` and `SwitchField.vue`
      (`Checkbox`/`Switch` with `Label`, `description` in
      `text-muted-foreground`), `EmptyState.vue`
      (`rounded-lg border border-dashed p-8 text-center`), `IconButton.vue`
      (`TooltipProvider` + `Tooltip` around `Button` `size="icon"` with
      `aria-label` and `aria-pressed`), `LoadingState.vue` (`role="status"`,
      `sr-only` label, `Skeleton` rows `h-4 w-full`).

- [ ] Delete `AppChrome.vue` and `AccountControl.vue`; point `app.vue` and
      `EditorShell.vue` at `AppShell`/`AccountMenu`/`ThemeToggle`.

- [ ] GREEN:

  ```sh
  cd apps/web && npx vitest run test/app test/logout-state.test.ts test/editor/theme.test.ts test/editor/editor-shell.test.ts
  make web-lint web-typecheck
  ```

## Adversarial checklist

- Author: the T03 cases in `adversarial-coverage.md`.

## Handoff

Report every composite's final prop list if it differs from the contract (it
should not), the `clear` producers typecheck reported, the `NativeSelect`
forwarding finding, RED and GREEN outputs. Suggested commit:
`feat(web): add the shared application components`.
