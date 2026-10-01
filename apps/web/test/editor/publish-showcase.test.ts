import { afterEach, describe, expect, it, vi } from 'vitest';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { DOMWrapper, mount, type VueWrapper } from '@vue/test-utils';
import { computed, nextTick, ref, type Ref } from 'vue';

import PublishDialog from '../../app/components/editor/PublishDialog.vue';
import type {
  ResumeEditorActions,
} from '../../app/composables/useResumeEditor';
import type {
  PublishControllerState,
} from '../../app/editor/publishController';
import { publishCopy } from '../../app/i18n/publish';
import { showcaseCopy } from '../../app/i18n/showcase';
import type { OwnerShowcase } from '../../app/lib/showcaseContract';
import type { ResumeRecord } from '../../app/stores/resumes';
import { acceptedFixture } from './fixture';

// The publish dialog's community showcase block: every state, disabled
// state, status line, and issue (docs/design/showcase.md, Copy;
// AC-SHOW-001, 003).

const locale = ref<'vi' | 'en'>('en');
mockNuxtImport('useLocale', () => () => ({ locale }));
const signInToViewCapability = ref(false);
mockNuxtImport('useCapabilities', () => () => ({
  signInToView: computed(() => signInToViewCapability.value),
}));

const mounted: VueWrapper[] = [];

afterEach(() => {
  locale.value = 'en';
  signInToViewCapability.value = false;
  for (const wrapper of mounted.splice(0)) wrapper.unmount();
});

function dialog(): DOMWrapper<Element> {
  const element = document.body.querySelector('[role="dialog"]');
  if (element === null) throw new Error('publish dialog is not mounted');
  return new DOMWrapper(element);
}

function record(
  metadata: Record<string, unknown> = {},
): ResumeRecord {
  const base = acceptedFixture();
  const accepted = acceptedFixture({
    metadata: { ...base.metadata, ...metadata } as typeof base.metadata,
  });
  return {
    accepted,
    current: {
      document: structuredClone(accepted.document),
      metadata: structuredClone(accepted.metadata),
    },
    pending: [],
    attempt: null,
    conflicts: [],
    issues: {},
    templateState: null,
    photoRead: { kind: 'none' },
    completeReadRequired: false,
    sessionLost: false,
    opaquePhotoOutcome: null,
  };
}

function actionsFor(
  initialState: PublishControllerState = { kind: 'idle' },
): {
  actions: ResumeEditorActions;
  state: Ref<PublishControllerState>;
  submit: ReturnType<typeof vi.fn>;
} {
  const state = ref<PublishControllerState>(initialState);
  const submit = vi.fn().mockResolvedValue(state.value);
  const publish = {
    state,
    submit,
    retryUncertain: vi.fn(),
    reauthPassword: vi.fn(),
    startProviderReauth: vi.fn(),
    retryAfterProviderReauth: vi.fn(),
    cancel: vi.fn(),
  };
  return { state, submit, actions: { publish: publish as never } as never };
}

async function open(
  metadata: Record<string, unknown>,
  initialState?: PublishControllerState,
) {
  const rec = record(metadata);
  const context = actionsFor(initialState);
  const wrapper = mount(PublishDialog, {
    attachTo: document.body,
    props: { open: true, actions: context.actions, record: rec },
  });
  mounted.push(wrapper);
  await nextTick();
  await nextTick();
  return context;
}

const OPEN = { live: true, slug: 'ada-lovelace' };
const switchSelector = '[data-action="publish-showcase"]';
const roleSelector = '[data-action="publish-showcase-role"]';

function showcase(state: OwnerShowcase['state'], role: string | null = null) {
  return { showcase: { state, role } };
}

async function click(selector: string): Promise<void> {
  await dialog().get(selector).trigger('click');
  await nextTick();
}

async function submit(): Promise<void> {
  await click('[data-action="publish-submit"]');
}

describe('publish showcase switch', () => {
  it.each(['en', 'vi'] as const)('labels the switch in %s', async (lng) => {
    locale.value = lng;
    await open(OPEN);
    const copy = publishCopy[lng].showcase;
    const control = dialog().get(switchSelector);
    expect(control.attributes('role')).toBe('switch');
    expect(control.attributes('aria-checked')).toBe('false');
    expect(control.attributes('disabled')).toBeUndefined();
    const block = dialog().get('[data-testid="publish-showcase"]');
    expect(block.text()).toContain(copy.label);
    expect(block.get('[data-testid="publish-showcase-description"]').text())
      .toBe(copy.help);
    expect(block.find('select').exists()).toBe(false);
  });

  it('sits after the sign-in switch, or after SEO and GEO without it',
    async () => {
      signInToViewCapability.value = true;
      await open(OPEN);
      const order = [...dialog().element.querySelectorAll('[role="switch"]')]
        .map((node) => node.getAttribute('data-action'));
      expect(order).toEqual([
        'publish-live',
        'publish-download',
        'publish-seo-geo',
        'publish-sign-in-to-view',
        'publish-showcase',
      ]);
    });

  it('shows no sign-in switch before the showcase when it is hidden',
    async () => {
      await open(OPEN);
      const order = [...dialog().element.querySelectorAll('[role="switch"]')]
        .map((node) => node.getAttribute('data-action'));
      expect(order.slice(-2)).toEqual(['publish-seo-geo', 'publish-showcase']);
    });

  it.each(['en', 'vi'] as const)(
    'disables the switch, shown off, while Public resume is off (%s)',
    async (lng) => {
      locale.value = lng;
      await open({ ...showcase('listed'), live: false });
      const control = dialog().get(switchSelector);
      expect(control.attributes('disabled')).toBeDefined();
      expect(control.attributes('aria-checked')).toBe('false');
      expect(dialog().get('[data-testid="publish-showcase-description"]')
        .text()).toBe(publishCopy[lng].showcase.needsPublic);
      expect(dialog().find('[data-testid="publish-showcase-status"]').exists())
        .toBe(false);
      expect(dialog().find(roleSelector).exists()).toBe(false);
    },
  );

  it.each(['en', 'vi'] as const)(
    'disables the switch, shown off, while sign in to view is on (%s)',
    async (lng) => {
      locale.value = lng;
      await open({ ...OPEN, ...showcase('listed'), signInToView: true });
      const control = dialog().get(switchSelector);
      expect(control.attributes('disabled')).toBeDefined();
      expect(control.attributes('aria-checked')).toBe('false');
      expect(dialog().get('[data-testid="publish-showcase-description"]')
        .text()).toBe(publishCopy[lng].showcase.needsOpenView);
    },
  );

  it('prefers the Public resume reason when both apply', async () => {
    await open({ live: false, signInToView: true });
    expect(dialog().get('[data-testid="publish-showcase-description"]')
      .text()).toBe(publishCopy.en.showcase.needsPublic);
  });

  it('disables the switch but keeps its value while a request runs',
    async () => {
      await open(
        { ...OPEN, ...showcase('listed') },
        { kind: 'saving' } as PublishControllerState,
      );
      const control = dialog().get(switchSelector);
      expect(control.attributes('disabled')).toBeDefined();
      expect(control.attributes('aria-checked')).toBe('true');
      expect(dialog().get('[data-testid="publish-showcase-description"]')
        .text()).toBe(publishCopy.en.showcase.help);
      expect(dialog().find('[data-testid="publish-showcase-status"]').exists())
        .toBe(false);
    });

  it('shows the stored value again when Public resume is undone',
    async () => {
      await open({ ...OPEN, ...showcase('listed') });
      expect(dialog().get(switchSelector).attributes('aria-checked'))
        .toBe('true');
      await click('[data-action="publish-live"]');
      expect(dialog().get(switchSelector).attributes('aria-checked'))
        .toBe('false');
      expect(dialog().get(switchSelector).attributes('disabled'))
        .toBeDefined();
      await click('[data-action="publish-live"]');
      expect(dialog().get(switchSelector).attributes('aria-checked'))
        .toBe('true');
      expect(dialog().get(switchSelector).attributes('disabled'))
        .toBeUndefined();
    });
});

describe('publish showcase status line', () => {
  it.each(['en', 'vi'] as const)('shows listed with its link in %s',
    async (lng) => {
      locale.value = lng;
      await open({ ...OPEN, ...showcase('listed') });
      const copy = publishCopy[lng].showcase;
      const status = dialog().get('[data-testid="publish-showcase-status"]');
      expect(status.text()).toBe(`${copy.listed} ${copy.listedLink}`);
      const link = status.get('a');
      expect(link.attributes('href')).toBe('/showcase');
      expect(link.attributes('target')).toBeUndefined();
      expect(link.classes()).toContain('text-link');
    });

  it('shows no status line for a switch just turned on', async () => {
    await open(OPEN);
    await click(switchSelector);
    expect(dialog().find('[data-testid="publish-showcase-status"]').exists())
      .toBe(false);
  });

  it('hides the status line once the owner turns the switch off',
    async () => {
      await open({ ...OPEN, ...showcase('listed') });
      await click(switchSelector);
      expect(dialog().find('[data-testid="publish-showcase-status"]').exists())
        .toBe(false);
    });

  it('updates the status from the published resource', async () => {
    const { state } = await open(OPEN);
    await click(switchSelector);
    state.value = {
      kind: 'accepted',
      resume: acceptedFixture({
        metadata: {
          ...acceptedFixture().metadata,
          ...OPEN,
          showcase: { state: 'listed', role: null },
        } as never,
      }),
    } as PublishControllerState;
    await nextTick();
    await nextTick();
    expect(dialog().get('[data-testid="publish-showcase-status"]').text())
      .toContain(publishCopy.en.showcase.listed);
  });
});

describe('publish showcase role', () => {
  it('lists None, the nine Library roles, then Other', async () => {
    await open({ ...OPEN, ...showcase('listed') });
    const select = dialog().get(roleSelector);
    const labels = select.findAll('option').map((option) => option.text());
    const roles = showcaseCopy.en.roles;
    expect(labels).toEqual([
      'None',
      roles.backend,
      roles.frontend,
      roles.mobile,
      roles.devops,
      roles['data-ai'],
      roles.qa,
      roles.fresher,
      roles.brse,
      roles.security,
      'Other',
    ]);
    expect(select.findAll('option').map((option) => option.attributes('value')))
      .toEqual([
        '', 'backend', 'frontend', 'mobile', 'devops', 'data-ai', 'qa',
        'fresher', 'brse', 'security', 'other',
      ]);
    const field = dialog().get('[data-field="showcaseRole"]');
    expect(field.text()).toContain('Role shown');
    expect(field.text()).toContain('Optional. Lets visitors filter by role.');
  });

  it('speaks Vietnamese for the role field', async () => {
    locale.value = 'vi';
    await open({ ...OPEN, ...showcase('listed', 'other') });
    const select = dialog().get(roleSelector);
    expect(select.findAll('option')[0]!.text()).toBe('Không chọn');
    expect(select.findAll('option').at(-1)!.text()).toBe('Khác');
    expect((select.element as HTMLSelectElement).value).toBe('other');
    expect(dialog().get('[data-field="showcaseRole"]').text())
      .toContain('Vị trí hiển thị');
  });

  it('appears when the owner turns the switch on', async () => {
    await open(OPEN);
    expect(dialog().find(roleSelector).exists()).toBe(false);
    await click(switchSelector);
    expect(dialog().find(roleSelector).exists()).toBe(true);
  });
});

describe('publish showcase command', () => {
  it('omits both fields when the owner changes neither', async () => {
    const { submit: send } = await open({
      ...OPEN,
      ...showcase('listed', 'qa'),
    });
    await submit();
    const sent = send.mock.lastCall![0] as Record<string, unknown>;
    expect(sent).not.toHaveProperty('showcaseEnabled');
    expect(sent).not.toHaveProperty('showcaseRole');
  });

  it('omits both fields on a resume that never opted in', async () => {
    const { submit: send } = await open(OPEN);
    await submit();
    const sent = send.mock.lastCall![0] as Record<string, unknown>;
    expect(sent).not.toHaveProperty('showcaseEnabled');
    expect(sent).not.toHaveProperty('showcaseRole');
  });

  it('sends only showcaseEnabled when the owner turns it on', async () => {
    const { submit: send } = await open(OPEN);
    await click(switchSelector);
    await submit();
    const sent = send.mock.lastCall![0] as Record<string, unknown>;
    expect(sent.showcaseEnabled).toBe(true);
    expect(sent).not.toHaveProperty('showcaseRole');
  });

  it('sends the role with the switch when the owner picks one', async () => {
    const { submit: send } = await open(OPEN);
    await click(switchSelector);
    await dialog().get(roleSelector).setValue('devops');
    await submit();
    expect(send.mock.lastCall![0]).toMatchObject({
      showcaseEnabled: true,
      showcaseRole: 'devops',
    });
  });

  it('sends only showcaseRole when the owner changes the role', async () => {
    const { submit: send } = await open({
      ...OPEN,
      ...showcase('listed', 'qa'),
    });
    await dialog().get(roleSelector).setValue('brse');
    await submit();
    const sent = send.mock.lastCall![0] as Record<string, unknown>;
    expect(sent.showcaseRole).toBe('brse');
    expect(sent).not.toHaveProperty('showcaseEnabled');
  });

  it('sends an empty role to clear it', async () => {
    const { submit: send } = await open({
      ...OPEN,
      ...showcase('listed', 'qa'),
    });
    await dialog().get(roleSelector).setValue('');
    await submit();
    const sent = send.mock.lastCall![0] as Record<string, unknown>;
    expect(sent.showcaseRole).toBe('');
    expect(sent).not.toHaveProperty('showcaseEnabled');
  });

  it('sends only showcaseEnabled false when the owner turns it off',
    async () => {
      const { submit: send } = await open({
        ...OPEN,
        ...showcase('listed', 'qa'),
      });
      await click(switchSelector);
      await submit();
      const sent = send.mock.lastCall![0] as Record<string, unknown>;
      expect(sent.showcaseEnabled).toBe(false);
      expect(sent).not.toHaveProperty('showcaseRole');
    });

  it('leaves the opt-in to the server when Public resume is turned off',
    async () => {
      const { submit: send } = await open({
        ...OPEN,
        ...showcase('listed', 'qa'),
      });
      await click('[data-action="publish-live"]');
      await submit();
      const sent = send.mock.lastCall![0] as Record<string, unknown>;
      expect(sent.live).toBe(false);
      expect(sent).not.toHaveProperty('showcaseEnabled');
      expect(sent).not.toHaveProperty('showcaseRole');
    });

  it('never sends showcaseEnabled true while the switch is disabled',
    async () => {
      const { submit: send } = await open(OPEN);
      await click(switchSelector);
      await dialog().get(roleSelector).setValue('qa');
      await click('[data-action="publish-live"]');
      await submit();
      const sent = send.mock.lastCall![0] as Record<string, unknown>;
      expect(sent).not.toHaveProperty('showcaseEnabled');
      expect(sent).not.toHaveProperty('showcaseRole');
    });
});

describe('publish showcase issues', () => {
  const invalid = (code: string, path = 'showcaseEnabled') => ({
    kind: 'invalid',
    issues: [{ path, code }],
  }) as PublishControllerState;

  it.each(['en', 'vi'] as const)(
    'shows requires_open_view at the block in %s',
    async (lng) => {
      locale.value = lng;
      await open(OPEN, invalid('requires_open_view'));
      const line = dialog().get('[data-testid="publish-showcase-issue"]');
      expect(line.attributes('role')).toBe('alert');
      expect(line.text()).toBe(publishCopy[lng].showcase.issueOpenView);
      expect(line.classes()).toContain('text-destructive');
      // Not in the dialog's own issue list.
      expect(dialog().find('[data-action="focus-publish-issue"]').exists())
        .toBe(false);
    },
  );

  it('shows requires_live with the dialog\'s existing text', async () => {
    await open(OPEN, invalid('requires_live'));
    expect(dialog().get('[data-testid="publish-showcase-issue"]').text())
      .toBe(publishCopy.en.issue.requires_live);
  });

  it('shows a role issue at the block with the generic text', async () => {
    await open(OPEN, invalid('invalid_format', 'showcaseRole'));
    expect(dialog().get('[data-testid="publish-showcase-issue"]').text())
      .toBe(publishCopy.en.invalid);
    expect(dialog().find('[data-action="focus-publish-issue"]').exists())
      .toBe(false);
  });

  it('clears the issue when the owner changes a showcase control',
    async () => {
      await open({ ...OPEN, ...showcase('listed') },
        invalid('requires_open_view'));
      expect(dialog().find('[data-testid="publish-showcase-issue"]').exists())
        .toBe(true);
      await dialog().get(roleSelector).setValue('qa');
      expect(dialog().find('[data-testid="publish-showcase-issue"]').exists())
        .toBe(false);
    });

  it('lists the description, status, and issue ids on the switch in order',
    async () => {
      await open({ ...OPEN, ...showcase('listed') },
        invalid('requires_open_view'));
      const describedBy = dialog().get(switchSelector)
        .attributes('aria-describedby')!.split(' ');
      expect(describedBy).toHaveLength(3);
      const texts = describedBy.map(
        (id) => dialog().element.querySelector(`[id="${id}"]`)?.getAttribute(
          'data-testid'),
      );
      expect(texts).toEqual([
        'publish-showcase-description',
        'publish-showcase-status',
        'publish-showcase-issue',
      ]);
    });

  it('lists only the description when nothing else shows', async () => {
    await open(OPEN);
    expect(dialog().get(switchSelector).attributes('aria-describedby')!
      .split(' ')).toHaveLength(1);
  });

  it('keeps other issues in the dialog list', async () => {
    await open(OPEN, {
      kind: 'invalid',
      issues: [{ path: 'slug', code: 'reserved' }],
    } as PublishControllerState);
    expect(dialog().find('[data-testid="publish-showcase-issue"]').exists())
      .toBe(false);
    expect(dialog().find('[data-action="focus-publish-issue"]').exists())
      .toBe(true);
  });
});
