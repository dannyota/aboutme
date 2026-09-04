# PV T05 — resume list

## Contract

Rebuild `apps/web/app/components/editor/list/ResumeList.vue` and the list page
as sheets on the desk with publish state.

### Data

`useResumeList` already returns `ResumeSummary` items with `live`, `slug`, and
`updatedAt`. Add `apps/web/app/utils/relativeTime.ts`:

```ts
export function formatRelativeTime(iso: string, now: Date): string;
```

Returns "just now" under 60 s, "{n} minutes ago", "{n} hours ago", "{n} days
ago" up to 6 days, else the date as `d MMM yyyy` in English ("2 Sep 2026");
singular forms for 1; an unparseable `iso` returns `iso` unchanged. Callers pass
`now` from a `useNow()` composable that reads `Date.now()` once per render;
tests pass a fixed date.

### Markup

```vue
<section aria-labelledby="resume-list-title" data-testid="resume-list">
  <PageHeader>
    <h1 id="resume-list-title">Resumes</h1>
    <Button data-testid="create-resume" :disabled="items.length >= 3" @click="$emit('create')">Create resume</Button>
  </PageHeader>
  <ul aria-label="Your resumes" class="grid gap-6 md:grid-cols-3">
    <li v-for="item in items" :data-testid="`resume-row-${item.id}`" class="sheet relative">   <!-- white, radius-dialog, shadow-paper -->
      <NuxtLink :to="`/app/resumes/${encodeURIComponent(item.id)}`" class="sheet-face after:absolute after:inset-0">
        <span class="text-lg font-semibold">{{ item.title }}</span>
        <span class="text-xs text-muted-foreground tabular-nums">Updated {{ formatRelativeTime(item.updatedAt, now) }}</span>
      </NuxtLink>
      <span class="relative z-10">
        <StateMark v-if="item.live && item.slug" state="public" :link="`/${item.slug}`" />
        <StateMark v-else state="draft" />
      </span>
      <DropdownMenu class="relative z-10">   <!-- trigger: IconButton aria-label="More actions for {title}" -->
        <DropdownMenuItem :aria-label="`Rename ${item.title}`" @select="$emit('rename', item)">Rename</DropdownMenuItem>
        <DropdownMenuItem :aria-label="`Delete ${item.title}`" class="text-destructive" @select="$emit('remove', item)">Delete</DropdownMenuItem>
      </DropdownMenu>
    </li>
    <li v-for="n in 3 - items.length" :key="`slot-${n}`" class="sheet sheet--empty">  <!-- dashed --border, no shadow -->
      <button type="button" @click="$emit('create')">
        <template v-if="items.length === 0 && n === 1">
          <span role="status">No resumes yet.</span>
          <span>Create your first resume. You can keep up to three.</span>
        </template>
        <span v-else>Create resume</span>
      </button>
    </li>
  </ul>
</section>
```

The `sheet-face` link covers the sheet through its stretched pseudo-element. The
public state link and menu are sibling interactive elements above that
pseudo-element, never anchors nested inside the editor link. The menu trigger
sits in the top-right corner. Busy items (`busyIds`) disable the menu trigger.
Hook changes: the old row buttons "Rename"/"Delete" move into the menu with the
same `aria-label`s; `role="status"` text "No resumes yet." is held. The dialogs
(`CreateResumeDialog`, `RenameResumeDialog`, `DeleteResumeDialog`) already use
PU's `FormDialog`/`ConfirmDialog`; keep them.

## TDD cases

Write `test/app/relative-time.test.ts` first with the table above, including
`now` injection and the invalid-ISO case. Update
`test/editor/resume-list.test.ts`: three items render three sheets and no empty
slot; one item renders two dashed slots; zero items render the status text in
the first slot; a public item shows the seal mark and `aboutme.vn/{slug}` from
the summary only; a draft shows "Draft"; a hostile title `<script>x</script>`
renders as text; Enter on the sheet link navigates; opening the menu and
choosing Rename emits without navigating and returns focus to the trigger on
Escape; "Create resume" is disabled at three items.

## Ownership and checks

Owned paths:

- `apps/web/app/components/editor/list/ResumeList.vue`
- `apps/web/app/pages/app/resumes/index.vue`
- `apps/web/app/utils/relativeTime.ts`, `apps/web/app/composables/useNow.ts`
- `apps/web/test/editor/resume-list.test.ts`, `test/app/relative-time.test.ts`

Acceptance: `AC-UI-009`.

Run:

```sh
cd apps/web
npx vitest run test/editor/resume-list.test.ts test/app/relative-time.test.ts
npx eslint app/components/editor/list app/pages/app/resumes/index.vue app/utils/relativeTime.ts app/composables/useNow.ts test/editor/resume-list.test.ts test/app/relative-time.test.ts
npx vue-tsc --noEmit
```

Do not add thumbnails, list-side publish actions, or API changes. Report the
first failing test, exact commands, and any `ResumeSummary` field you needed
that the client type lacks.
