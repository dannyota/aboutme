# Phase PU component contracts

Frozen props, emits, and slots of the `app/components/app` composites. T03
implements them; T04–T12 consume them. A change to a frozen name or type is an
owner-window change, not a worker edit.

Every composite: `<script setup lang="ts">`, no `<style>` block, explicit
imports from `@/components/ui/<name>`, `cn()` from `@/lib/utils`, and a `class`
pass-through to its root element.

## AppShell.vue

Replaces `AppChrome.vue` and `AccountControl.vue`.

- Props: none. Reads `useAuth().authState` and `useAuth().user`.
- Renders `<header data-testid="app-shell">` with the brand link (text
  `aboutme`, `to="/"`), a `<nav aria-label="Primary navigation">` with `Resumes`
  (`/app/resumes`) and `Settings` (`/app/settings/sessions`) when authenticated,
  `AccountMenu` and `ThemeToggle` when authenticated, and the `Sign in`
  (`/login`) and `Create account` (`/register`) links plus `ThemeToggle`
  otherwise. Until the session read resolves it renders the signed-out variant.
- Active nav link carries `aria-current="page"`.

## AccountMenu.vue

- Props: none. Reads `useAuth().user` and `useAuth().logout`.
- Trigger: `Button` variant `ghost` with `Avatar` (initials from the name,
  fallback icon) and the name or email, `aria-label`
  `Account settings for {name}`, `data-testid="account-menu"`.
- Items: `Settings` (navigates to `/app/settings/sessions`) and `Log out` (calls
  `logout()`), `data-testid="account-menu-settings"` and
  `data-testid="account-menu-logout"`.

## ThemeToggle.vue

- Props: none. Uses `useTheme()`.
- Renders `Button` variant `ghost` size `sm` with `aria-label`
  `Switch to {next} theme`, the Sun or Moon icon, and the visible text
  `Light mode` or `Dark mode` (hidden below `md`).

## PageHeader.vue

- Props: `title: string`, `description?: string`, `titleId?: string` (default
  `page-title`).
- Slots: `actions` (right-aligned).
- Renders `<h1 :id="titleId" class="text-2xl font-semibold tracking-tight">` and
  the description in `text-muted-foreground`.

## FormField.vue

Owns ids and ARIA wiring for one control.

- Props: `label: string`, `id?: string` (default from `useId()`),
  `hint?: string`, `error?: string`, `required?: boolean`, `name?: string`
  (rendered as `data-field` on the wrapper),
  `errorAttrs?: Record<string, string>` (applied to the error paragraph).
- Slot `default` with slot props
  `{ id: string; describedBy: string | undefined; invalid: true | undefined }`.
- Renders `Label :for="id"`, the slot,
  `<p :id="`${id}-hint`">` when `hint`,
  and `<p :id="`${id}-error`" role="alert" :data-error-for="name">`
  when `error`. `describedBy` joins the hint and error ids that exist.

## TextField.vue

- Props: `label: string`, `modelValue?: string`, `id?: string`, `name?: string`,
  `type?: 'text' | 'email' | 'url'` (default `text`), `multiline?: boolean`,
  `rows?: number` (default 3), `autocomplete?: string`, `inputmode?: string`,
  `placeholder?: string`, `hint?: string`, `error?: string`,
  `errorAttrs?: Record<string, string>`, `required?: boolean`,
  `disabled?: boolean`, `controlAttrs?: Record<string, string>`.
- Emits: `intent: [intent: FieldIntent<string>]` following
  [U4](decisions.md#u4--field-commit-rule) exactly.
- Renders `FormField` around `Input` or `Textarea`. The control carries
  `data-field-input` plus every entry of the optional
  `controlAttrs?: Record<string, string>` prop (used for `data-detail-*` and
  `data-part` hooks that tests read on the input). `errorAttrs` passes through
  to the `FormField` error paragraph. Other attributes fall through to the root
  wrapper, so `data-entry-field` lands on the wrapper.
- Exposes `focus(): void`.

## SelectField.vue

- Props: `label: string`, `modelValue: string | number`,
  `options: readonly { value: string | number; label: string }[]`,
  `id?: string`, `name?: string`, `hint?: string`, `error?: string`,
  `disabled?: boolean`, `controlAttrs?: Record<string, string>`.
- Emits: `update:modelValue: [value: string]` on `change`. The consumer casts
  numeric enumerations.
- Renders `FormField` around `NativeSelect` with one `NativeSelectOption` per
  option. `controlAttrs?: Record<string, string>` lands on the `<select>`; other
  attributes fall through to the root wrapper.

## CheckboxField.vue

- Props: `label: string`, `modelValue: boolean`, `id?: string`, `name?: string`,
  `description?: string`, `disabled?: boolean`.
- Emits: `update:modelValue: [value: boolean]`.
- Renders reka `Checkbox` (`button[role="checkbox"]`) with `Label`. Attributes
  such as `data-action` fall through to the checkbox.

## SwitchField.vue

- Props: `label: string`, `modelValue: boolean`, `id?: string`,
  `description?: string`, `disabled?: boolean`.
- Emits: `update:modelValue: [value: boolean]`.
- Renders `Switch` (`button[role="switch"]`) with `Label`. Attributes fall
  through to the switch.

## ConfirmDialog.vue

- Props: `open: boolean`, `title: string`, `description: string`,
  `confirmLabel: string`, `cancelLabel?: string` (default `Cancel`),
  `destructive?: boolean`, `busy?: boolean`, `confirmText?: string`,
  `confirmInputLabel?: string` (default `Type {confirmText} to confirm`),
  `confirmAction?: string`, `cancelAction?: string`.
- Emits: `confirm: []`, `cancel: []`.
- Renders `AlertDialog` with two plain `Button`s in the footer (never
  `AlertDialogAction`, which would close the controlled dialog and emit a
  cancel). When `confirmText` is set, an `Input` labelled `confirmInputLabel`
  gates the confirm button (`disabled` until the value equals `confirmText`
  exactly; the entered text is cleared on close). Buttons carry `data-action`
  from `confirmAction` and `cancelAction`. Escape and `update:open(false)` emit
  `cancel` unless `busy`. Focus lands on the cancel button when `destructive`,
  else on confirm, and returns to the opener on close.

## FormDialog.vue

- Props: `open: boolean`, `title: string`, `description?: string`,
  `submitLabel: string`, `cancelLabel?: string` (default `Cancel`),
  `busy?: boolean`, `submitDisabled?: boolean`, `submitAction?: string`,
  `cancelAction?: string`.
- Emits: `submit: []`, `cancel: []`.
- Slots: `default` (form body), `footer` (replaces the two buttons).
- Renders `Dialog` around `<form novalidate @submit.prevent="emit('submit')">`.
  The first focusable control in the body receives focus on open. Escape and the
  overlay emit `cancel` unless `busy`. Focus returns to the opener on close.

## StatusBanner.vue

- Props: `kind: 'info' | 'success' | 'error'`, `title?: string`,
  `testid?: string`, `focusOnMount?: boolean`.
- Slot `default`.
- Renders `Alert` with `role="alert"` for `error` and `role="status"` otherwise,
  `tabindex="-1"`, `data-testid` from `testid`, and the matching icon.
  `focusOnMount` calls `focus()` after mount.

## EmptyState.vue

- Props: `title: string`, `description?: string`.
- Slot `action`.
- Renders a bordered, centered block with `text-muted-foreground` copy.

## IconButton.vue

- Props: `label: string`, `variant?: ButtonVariants['variant']` (default
  `ghost`), `size?: 'icon' | 'icon-sm'` (default `icon`), `pressed?: boolean`,
  `disabled?: boolean`.
- Slot `default` (the icon).
- Renders `Tooltip` around `Button` with `aria-label="label"` and `aria-pressed`
  when `pressed` is defined. Attributes fall through to the button.

## LoadingState.vue

- Props: `label: string`, `lines?: number` (default 3), `testid?: string`.
- Renders `<div role="status">` with a visually hidden label and `lines`
  `Skeleton` rows.

## InspectorPanel.vue (editor)

Lives in `app/components/editor/`.

- Props: `title: string`, `titleId: string`, `description?: string`.
- Slots: `actions`, `default`.
- Renders `<section :aria-labelledby="titleId" class="flex flex-col gap-4">`
  with an `<h2 :id="titleId" class="text-lg font-semibold">`.

## EntryCard.vue (editor)

- Props: `title: string`, `subtitle?: string`, `entryId: string`,
  `hidden: boolean`, `index: number`, `count: number`, `open?: boolean`.
- Emits: `toggleHidden: []`, `moveUp: []`, `moveDown: []`, `delete: []`,
  `update:open: [open: boolean]`.
- Slot `default` (the fields).
- Renders `Collapsible` in a `Card`. The header holds the title, a dedicated
  `CollapsibleTrigger` `IconButton` labelled `Collapse entry fields` or
  `Expand entry fields` (`data-action="toggle-entry-fields"`), a `Switch`
  labelled `Hidden` (`button[role="switch"]`, `data-action="toggle-hidden"`,
  `aria-checked` mirrors `hidden`), and sibling `IconButton`s `Move entry up`
  (`data-action="entry-up"`, disabled at index 0), `Move entry down`
  (`data-action="entry-down"`, disabled at the last index), and `Delete entry`
  (`data-action="delete-entry"`). The trigger never wraps those interactive
  siblings. The root carries `data-entry-id`.

## PreviewToolbar.vue (editor)

- Props: `estimatedPages: number | null`, `zoom: 'fit' | 'full'`,
  `photoState: 'ready' | 'loading' | 'unavailable' | 'none'`.
- Emits: `update:zoom: [zoom: 'fit' | 'full']`, `openPhoto: []`.
- Renders the `Preview` heading, the `Estimated pages` label with
  `<output aria-label="Estimated page count">`, a `ToggleGroup` for zoom, and,
  when `photoState` is `loading` or `unavailable`, a `Badge` with the state text
  and a `Button` variant `link` (`Open photo panel`) that emits `openPhoto`.
