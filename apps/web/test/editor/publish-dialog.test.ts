import { afterEach, describe, expect, it, vi } from 'vitest';
import { DOMWrapper, mount, type VueWrapper } from '@vue/test-utils';
import { computed, nextTick, ref, type Ref } from 'vue';

import PublishDialog from '../../app/components/editor/PublishDialog.vue';
import type {
  ResumeEditorActions,
} from '../../app/composables/useResumeEditor';
import type { PublishCommand } from '../../app/editor/publishApi';
import type {
  PublishControllerState,
} from '../../app/editor/publishController';
import type { ResumeRecord } from '../../app/stores/resumes';
import { acceptedFixture } from './fixture';

const mounted: VueWrapper[] = [];

afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount();
});

class DialogHarness {
  constructor(
    readonly component: VueWrapper,
    private element: Element,
  ) {}

  private root(): DOMWrapper<Element> {
    if (!this.element.isConnected) {
      throw new Error('publish dialog is not mounted');
    }
    return new DOMWrapper(this.element);
  }

  get(selector: string) {
    const root = this.root();
    if (root.element.matches(selector)) return root;
    return root.get(selector);
  }

  find(selector: string) {
    return this.root().find(selector);
  }

  findAll(selector: string) {
    return this.root().findAll(selector);
  }

  text(): string {
    return this.root().text();
  }

  setProps(props: Record<string, unknown>) {
    return this.component.setProps(props);
  }

  emitted(name: string) {
    return this.component.emitted(name);
  }

  get vm() {
    return this.component.vm;
  }

  unmount(): void {
    this.component.unmount();
  }
}

function dialog(): DOMWrapper<Element> {
  const element = document.body.querySelector('[role="dialog"]');
  if (element === null) throw new Error('publish dialog is not mounted');
  return new DOMWrapper(element);
}

describe('PublishDialog', () => {
  it('renders exactly the three product choices and both disclosures',
    async () => {
      const record = editorRecord();
      const { actions } = actionsFor(record);
      await mountDialog(record, actions);

      const choices = dialog().findAll('[role="switch"]');
      expect(choices).toHaveLength(3);
      expect(dialog().text()).toContain('Public resume');
      expect(dialog().text()).toContain('PDF download');
      expect(dialog().text()).toContain('SEO and GEO');
      expect(dialog().text()).toContain(
        'Public resumes may be delivered through a global '
        + 'content-delivery network.',
      );
      expect(dialog().text()).toContain(
        'SEO and GEO allow search crawlers and AI answer engines to discover '
        + 'and reuse public resume content.',
      );
    });

  it('bounds and scrolls long dialog content within the viewport', async () => {
    const record = editorRecord();
    const context = actionsFor(record, {
      kind: 'invalid',
      issues: Array.from({ length: 20 }, (_, index) => ({
        path: `content.work.entries.${index}.title`,
        code: 'required',
        message: 'server text is not rendered',
      })),
    });
    const wrapper = await mountDialog(record, context.actions);

    expect(wrapper.get('[role="dialog"]').classes()).toEqual(
      expect.arrayContaining([
        'max-h-[calc(100dvh-2rem)]',
        'overflow-y-auto',
        'sm:max-w-[38rem]',
      ]),
    );
    expect(wrapper.findAll('[data-action="focus-publish-issue"]'))
      .toHaveLength(20);
    expect(wrapper.get('[data-action="publish-close"]').exists()).toBe(true);
  });

  it('initializes never-published and canonical publication metadata',
    async () => {
      const neverPublished = editorRecord();
      const never = actionsFor(neverPublished);
      const wrapper = await mountDialog(neverPublished, never.actions);
      expect(
        wrapper.get('[data-action="publish-live"]')
          .attributes('aria-checked'),
      ).toBe('false');
      expect(
        wrapper.get('[data-action="publish-download"]')
          .attributes('aria-checked'),
      ).toBe('false');
      expect(
        wrapper.get('[data-action="publish-seo-geo"]')
          .attributes('aria-checked'),
      ).toBe('false');

      const published = editorRecord({
        live: true,
        downloadEnabled: true,
        seoGeoEnabled: true,
        slug: 'ada-lovelace',
      });
      const existing = actionsFor(published);
      await wrapper.setProps({ open: false });
      await wrapper.setProps({
        open: true,
        record: published,
        actions: existing.actions,
      });
      expect(
        wrapper.get('[data-action="publish-live"]')
          .attributes('aria-checked'),
      ).toBe('true');
      expect(
        wrapper.get('[data-action="publish-download"]')
          .attributes('aria-checked'),
      ).toBe('true');
      expect(
        wrapper.get('[data-action="publish-seo-geo"]')
          .attributes('aria-checked'),
      ).toBe('true');
      expect(
        (wrapper.get('[data-action="publish-slug"]')
          .element as HTMLInputElement)
          .value,
      ).toBe('ada-lovelace');
    });

  it('turning Public resume off clears and disables both dependent choices',
    async () => {
      const record = editorRecord({
        live: true,
        downloadEnabled: true,
        seoGeoEnabled: true,
        slug: 'ada-lovelace',
      });
      const { actions } = actionsFor(record);
      const wrapper = await mountDialog(record, actions);
      await wrapper.get('[data-action="publish-live"]').trigger('click');

      for (const action of ['publish-download', 'publish-seo-geo']) {
        const input = wrapper.get(`[data-action="${action}"]`);
        expect(input.attributes('aria-checked')).toBe('false');
        expect(input.attributes('disabled')).toBeDefined();
      }
    });

  it('labels the primary action from the canonical initial publication state',
    async () => {
      const privateRecord = editorRecord();
      const privateContext = actionsFor(privateRecord);
      const privateWrapper = await mountDialog(
        privateRecord,
        privateContext.actions,
      );
      expect(privateWrapper.get('[data-action="publish-submit"]').text())
        .toBe('Publish');

      const liveRecord = editorRecord({ live: true, slug: 'ada-lovelace' });
      const liveContext = actionsFor(liveRecord);
      const liveWrapper = await mountDialog(liveRecord, liveContext.actions);
      expect(liveWrapper.get('[data-action="publish-submit"]').text())
        .toBe('Update publication');
      await liveWrapper.get('[data-action="publish-live"]').trigger('click');
      expect(liveWrapper.get('[data-action="publish-submit"]').text())
        .toBe('Unpublish');
    });

  it('validates every edited slug boundary and permits blank only when private',
    async () => {
      const record = editorRecord();
      const { actions } = actionsFor(record);
      const wrapper = await mountDialog(record, actions);
      const live = wrapper.get('[data-action="publish-live"]');
      await live.trigger('click');
      const slug = wrapper.get('[data-action="publish-slug"]');
      const submit = wrapper.get('[data-action="publish-submit"]');

      for (const value of [
        '',
        'abc',
        'a'.repeat(31),
        'Ada-lovelace',
        'a_bcd',
        '-abcd',
        'abcd-',
        'a--b',
      ]) {
        await slug.setValue(value);
        expect(submit.attributes('disabled')).toBeDefined();
      }
      for (const value of ['abcd', 'a'.repeat(30), 'a-b9']) {
        await slug.setValue(value);
        expect(submit.attributes('disabled')).toBeUndefined();
      }

      await live.trigger('click');
      await slug.setValue('abc');
      expect(wrapper.text()).toContain('Use 4–30 lowercase ASCII letters');
      expect(submit.attributes('disabled')).toBeDefined();
      await slug.setValue('ada-lovelace');
      await wrapper.get('[data-action="publish-submit"]').trigger('click');
      expect(actions.publish.submit).toHaveBeenCalledWith({
        slug: 'ada-lovelace',
        live: false,
        downloadEnabled: false,
        seoGeoEnabled: false,
      });
    });

  it('submits the exact frozen command through the controller', async () => {
    const record = editorRecord();
    const { actions } = actionsFor(record);
    const wrapper = await mountDialog(record, actions);
    await wrapper.get('[data-action="publish-slug"]').setValue('ada-lovelace');
    await wrapper.get('[data-action="publish-live"]').trigger('click');
    await wrapper.get('[data-action="publish-download"]').trigger('click');
    await wrapper.get('[data-action="publish-seo-geo"]').trigger('click');
    await wrapper.get('[data-action="publish-submit"]').trigger('click');

    expect(actions.publish.submit).toHaveBeenCalledOnce();
    expect(actions.publish.submit).toHaveBeenCalledWith({
      slug: 'ada-lovelace',
      live: true,
      downloadEnabled: true,
      seoGeoEnabled: true,
    } satisfies PublishCommand);
  });

  it('shows save-first busy state and refuses Escape and Close while busy',
    async () => {
      const record = editorRecord();
      const context = actionsFor(record, { kind: 'saving' });
      const wrapper = await mountDialog(record, context.actions);
      await wrapper.get('[role="dialog"]').trigger('keydown.esc');
      await wrapper.get('[data-action="publish-close"]').trigger('click');

      expect(wrapper.get('[role="dialog"]').exists()).toBe(true);
      expect(context.actions.publish.cancel).not.toHaveBeenCalled();
      expect(wrapper.text()).toContain('Publishing…');
    });

  it('closes and emits issue focus without rendering server text', async () => {
    const record = editorRecord();
    const context = actionsFor(record, {
      kind: 'invalid',
      issues: [
        {
          path: 'personalDetails.fullName',
          code: 'required',
          message: 'secret server detail',
        },
      ],
    });
    const wrapper = await mountDialog(record, context.actions);
    expect(wrapper.text()).not.toContain('secret server detail');
    await wrapper.get('[data-action="focus-publish-issue"]').trigger('click');
    expect(wrapper.emitted('close')).toHaveLength(1);
    expect(wrapper.emitted('focus-issue')).toEqual([
      ['personalDetails.fullName'],
    ]);
  });

  it('selects the password factor and resumes the controller operation',
    async () => {
      const record = editorRecord();
      const context = actionsFor(record, {
        kind: 'reauth-required',
        method: 'password',
        attempt: {} as never,
      });
      const wrapper = await mountDialog(record, context.actions);
      await wrapper.get('[data-action="publish-password"]')
        .setValue('current-password');
      await wrapper
        .get('[data-action="publish-password-reauth"]')
        .trigger('click');

      expect(context.actions.publish.reauthPassword).toHaveBeenCalledWith(
        'current-password',
      );
      expect(context.actions.publish.submit).not.toHaveBeenCalled();
    });

  it('uses provider start and explicit post-round-trip retry actions',
    async () => {
      const record = editorRecord();
      const context = actionsFor(record, {
        kind: 'reauth-required',
        method: 'provider',
        attempt: {} as never,
      });
      const wrapper = await mountDialog(record, context.actions);
      const openSpy = vi.spyOn(window, 'open').mockImplementation(() => null);
      await wrapper
        .get('[data-action="publish-provider-start"]')
        .trigger('click');
      expect(context.actions.publish.startProviderReauth)
        .toHaveBeenCalledOnce();
      expect(openSpy).not.toHaveBeenCalled();

      context.state.value = {
        kind: 'provider-started',
        authorizeUrl: 'https://accounts.google.com/o/oauth2/auth?x=1',
        attempt: {} as never,
      };
      await wrapper.vm.$nextTick();
      const link = wrapper.get('[data-action="publish-provider-link"]');
      expect(link.attributes('target')).toBe('_blank');
      expect(link.attributes('rel')).toBe('noopener noreferrer');
      await link.trigger('click');
      expect(openSpy).not.toHaveBeenCalled();
      expect(wrapper.text()).toContain('return to the editor');
      await wrapper
        .get('[data-action="publish-provider-retry"]')
        .trigger('click');
      expect(
        context.actions.publish.retryAfterProviderReauth,
      ).toHaveBeenCalledOnce();
      openSpy.mockRestore();
    });

  it('does not render a provider anchor for missing or rejected starts',
    async () => {
      const record = editorRecord();
      const context = actionsFor(record, {
        kind: 'provider-start-invalid',
        method: 'provider',
        attempt: {} as never,
      });
      const wrapper = await mountDialog(record, context.actions);
      expect(
        wrapper.find('[data-action="publish-provider-link"]').exists(),
      ).toBe(false);
      expect(wrapper.text()).toContain('Reauthentication is unavailable.');
      expect(wrapper.findAll('[data-action="publish-provider-link"]'))
        .toHaveLength(0);
      expect(wrapper.findAll('[data-action="publish-provider-start"]'))
        .toHaveLength(1);
    });

  it('keeps stale review on a fresh submit and unknown on uncertain retry',
    async () => {
      const record = editorRecord({ live: true, slug: 'ada-lovelace' });
      const stale = actionsFor(record, {
        kind: 'stale',
        winner: {} as never,
      });
      const staleWrapper = await mountDialog(record, stale.actions);
      expect(staleWrapper.findAll('[data-action="publish-retry"]'))
        .toHaveLength(0);
      await staleWrapper.get('[data-action="publish-submit"]')
        .trigger('click');
      expect(stale.actions.publish.submit).toHaveBeenCalledOnce();

      const unknown = actionsFor(record, {
        kind: 'unknown',
        reason: 'transport',
      });
      const unknownWrapper = await mountDialog(record, unknown.actions);
      expect(unknownWrapper.find('[data-action="publish-submit"]').exists())
        .toBe(false);
      await unknownWrapper.get('[data-action="publish-retry"]')
        .trigger('click');
      expect(unknown.actions.publish.retryUncertain).toHaveBeenCalledOnce();
    });

  it('resynchronizes inputs after a stale complete read', async () => {
    const record = editorRecord({
      live: true,
      downloadEnabled: true,
      seoGeoEnabled: false,
      slug: 'old-slug',
    });
    const context = actionsFor(record);
    const wrapper = await mountDialog(record, context.actions);

    record.accepted = acceptedFixture({
      metadata: {
        ...record.accepted.metadata,
        live: true,
        downloadEnabled: false,
        seoGeoEnabled: true,
        slug: 'concurrent-winner',
      },
      metadataFreshness: 'complete',
    });
    context.state.value = {
      kind: 'stale',
      winner: {
        document: record.accepted.document,
        revision: record.accepted.revision,
      },
    };
    await wrapper.vm.$nextTick();

    expect(
      wrapper.get('[data-action="publish-download"]')
        .attributes('aria-checked'),
    ).toBe('false');
    expect(
      wrapper.get('[data-action="publish-seo-geo"]')
        .attributes('aria-checked'),
    ).toBe('true');
    expect(
      (wrapper.get('[data-action="publish-slug"]')
        .element as HTMLInputElement).value,
    ).toBe('concurrent-winner');
  });

  it('uses fixed copy and one intended recovery path for terminal states',
    async () => {
      const record = editorRecord({ live: true, slug: 'ada-lovelace' });
      const cases: Array<{
        name: string;
        state: PublishControllerState;
        recovery?: string;
        forbidden?: string;
        expectedCopy?: string;
        primaryDisabled?: boolean;
      }> = [
        {
          name: 'not loaded',
          state: { kind: 'blocked', reason: 'not-loaded' },
          expectedCopy: 'The resume is still loading.',
          primaryDisabled: true,
        },
        {
          name: 'saving',
          state: { kind: 'blocked', reason: 'saving' },
          expectedCopy: 'Save the latest resume changes',
          primaryDisabled: true,
        },
        {
          name: 'conflict',
          state: { kind: 'blocked', reason: 'conflict' },
          expectedCopy: 'Resolve the resume conflict',
          primaryDisabled: true,
        },
        {
          name: 'blocked session lost',
          state: { kind: 'blocked', reason: 'session-lost' },
          expectedCopy: 'Your session ended.',
          primaryDisabled: true,
        },
        {
          name: 'issue',
          state: { kind: 'blocked', reason: 'issue' },
          expectedCopy: 'Resolve the current resume issues',
          primaryDisabled: true,
        },
        {
          name: 'partial template',
          state: { kind: 'blocked', reason: 'partial-template' },
          expectedCopy: 'Finish recovering the template changes',
          primaryDisabled: true,
        },
        {
          name: 'opaque photo',
          state: { kind: 'blocked', reason: 'opaque-photo' },
          expectedCopy: 'Resolve the photo change',
          primaryDisabled: true,
        },
        {
          name: 'read required',
          state: { kind: 'blocked', reason: 'read-required' },
          expectedCopy: 'Refresh the complete resume',
          primaryDisabled: true,
        },
        {
          name: 'slug taken',
          state: { kind: 'slug-taken' },
          recovery: 'Retry publish',
        },
        {
          name: 'stale',
          state: { kind: 'stale', winner: {} as never },
          forbidden: 'Retry publish',
        },
        {
          name: 'rate limited',
          state: { kind: 'rate-limited', retryAfterMs: null },
          recovery: 'Retry publish',
        },
        {
          name: 'public busy',
          state: { kind: 'public-state-busy', retryAfterMs: null },
          recovery: 'Retry publish',
        },
        { name: 'session lost', state: { kind: 'session-lost' } },
        {
          name: 'provider disabled',
          state: { kind: 'failed', code: 'provider_disabled' as never },
        },
        {
          name: 'csrf failure',
          state: { kind: 'failed', code: 'csrf_rejected' as never },
        },
        {
          name: 'save failure',
          state: { kind: 'failed', code: 'save_failed' as never },
        },
        {
          name: 'generic failure',
          state: { kind: 'failed', code: 'request_invalid' },
        },
        {
          name: 'wrong password',
          state: {
            kind: 'reauth-wrong-password',
            method: 'password',
            attempt: {} as never,
          },
        },
        {
          name: 'password rate',
          state: {
            kind: 'reauth-rate-limited',
            method: 'password',
            attempt: {} as never,
          },
        },
        {
          name: 'provider rate',
          state: {
            kind: 'reauth-rate-limited',
            method: 'provider',
            attempt: {} as never,
          },
        },
        {
          name: 'password unavailable',
          state: {
            kind: 'reauth-unavailable',
            method: 'password',
            attempt: {} as never,
          },
        },
        {
          name: 'provider unavailable',
          state: {
            kind: 'reauth-unavailable',
            method: 'provider',
            attempt: {} as never,
          },
        },
        {
          name: 'provider invalid',
          state: {
            kind: 'provider-start-invalid',
            method: 'provider',
            attempt: {} as never,
          },
          recovery: 'Try provider reauthentication again',
          forbidden: 'Retry publish',
        },
        {
          name: 'provider start rate',
          state: {
            kind: 'provider-started-rate-limited',
            method: 'provider',
            attempt: {} as never,
          },
          recovery: 'Try provider reauthentication again',
          forbidden: 'Retry publish',
        },
        {
          name: 'unknown',
          state: { kind: 'unknown', reason: 'transport' },
          recovery: 'Retry publish',
          forbidden: 'Publish',
        },
      ];

      for (const testCase of cases) {
        const context = actionsFor(record, testCase.state);
        const wrapper = await mountDialog(record, context.actions);
        expect(wrapper.text(), testCase.name)
          .not.toContain('secret server text');
        expect(wrapper.get('[role="alert"]').exists(), testCase.name)
          .toBe(true);
        if (testCase.expectedCopy !== undefined) {
          expect(wrapper.text(), testCase.name)
            .toContain(testCase.expectedCopy);
        }
        if (testCase.primaryDisabled) {
          expect(
            wrapper.get('[data-action="publish-submit"]')
              .attributes('disabled'),
            testCase.name,
          ).toBeDefined();
        }
        if (testCase.recovery !== undefined) {
          expect(wrapper.text(), testCase.name)
            .toContain(testCase.recovery);
        }
        if (testCase.forbidden !== undefined) {
          if (testCase.forbidden === 'Publish') {
            expect(wrapper.find('[data-action="publish-submit"]').exists(),
              testCase.name).toBe(false);
          } else {
            expect(wrapper.text(), testCase.name)
              .not.toContain(testCase.forbidden);
          }
        }
      }
    });

  it('shows only a canonical public link for a live accepted result',
    async () => {
      const record = editorRecord();
      const liveAccepted = acceptedFixture({
        metadata: {
          ...acceptedFixture().metadata,
          live: true,
          slug: 'canonical-slug',
        },
      });
      const context = actionsFor(record, {
        kind: 'accepted',
        resume: liveAccepted,
      });
      const wrapper = await mountDialog(record, context.actions);
      expect(
        wrapper.get('[data-action="view-public-resume"]').attributes('href'),
      ).toBe('/canonical-slug');

      context.state.value = {
        kind: 'accepted',
        resume: acceptedFixture({
          metadata: {
            ...acceptedFixture().metadata,
            live: true,
            slug: '//evil.example',
          },
        }),
      };
      await wrapper.vm.$nextTick();
      expect(wrapper.find('[data-action="view-public-resume"]').exists())
        .toBe(false);

      context.state.value = {
        kind: 'accepted',
        resume: acceptedFixture({
          metadata: {
            ...acceptedFixture().metadata,
            live: false,
            slug: 'old-slug',
          },
        }),
      };
      await wrapper.vm.$nextTick();
      expect(wrapper.text()).toContain('Resume is private.');
      expect(wrapper.find('[data-action="view-public-resume"]').exists())
        .toBe(false);

      const transitionRecord = editorRecord({
        live: true,
        slug: 'old-slug',
      });
      const transition = actionsFor(transitionRecord);
      const transitionWrapper = await mountDialog(
        transitionRecord,
        transition.actions,
      );
      transition.state.value = {
        kind: 'accepted',
        resume: acceptedFixture({
          metadata: {
            ...acceptedFixture().metadata,
            live: false,
            slug: 'old-slug',
          },
        }),
      };
      await transitionWrapper.vm.$nextTick();
      expect(
        transitionWrapper.get('[data-action="publish-live"]')
          .attributes('aria-checked'),
      ).toBe('false');
      expect(transitionWrapper.get('[data-action="publish-submit"]').text())
        .toBe('Publish');

      const newRecord = editorRecord();
      const newPublication = actionsFor(newRecord);
      const newWrapper = await mountDialog(newRecord, newPublication.actions);
      newPublication.state.value = {
        kind: 'accepted',
        resume: acceptedFixture({
          metadata: {
            ...acceptedFixture().metadata,
            live: true,
            slug: 'new-slug',
          },
        }),
      };
      await newWrapper.vm.$nextTick();
      expect(newWrapper.get('[data-action="publish-submit"]').text())
        .toBe('Update publication');
    });

  it('returns focus on close and contains Tab focus inside the dialog',
    async () => {
      const opener = document.createElement('button');
      document.body.append(opener);
      opener.focus();
      const record = editorRecord();
      const context = actionsFor(record);
      const wrapper = await mountDialog(record, context.actions);
      await wrapper.vm.$nextTick();
      expect(document.activeElement).toBe(
        wrapper.get('[data-action="publish-slug"]').element,
      );

      await wrapper
        .get('[role="dialog"]')
        .trigger('keydown', { key: 'Tab', shiftKey: true });
      expect(document.activeElement).toBe(
        wrapper.get('[data-action="publish-close"]').element,
      );
      await wrapper.get('[role="dialog"]').trigger('keydown', { key: 'Tab' });
      expect(document.activeElement).toBe(
        wrapper.get('[data-action="publish-slug"]').element,
      );
      await wrapper.get('[data-action="publish-close"]').trigger('click');
      expect(wrapper.emitted('close')).toHaveLength(1);
      await wrapper.setProps({ open: false });
      await wrapper.vm.$nextTick();
      await new Promise((resolve) => setTimeout(resolve, 0));
      await wrapper.vm.$nextTick();
      expect(document.activeElement).toBe(opener);
      wrapper.unmount();
      opener.remove();
    });

  it('suppresses duplicate submit while the controller promise is pending',
    async () => {
      const record = editorRecord();
      const deferred = deferredPromise<undefined>();
      const context = actionsFor(record);
      vi.mocked(context.actions.publish.submit).mockReturnValue(
        deferred.promise as never,
      );
      const wrapper = await mountDialog(record, context.actions);
      await wrapper.get('[data-action="publish-slug"]')
        .setValue('ada-lovelace');
      await wrapper.get('[data-action="publish-live"]').trigger('click');
      await wrapper.get('[data-action="publish-submit"]').trigger('click');
      await wrapper.get('[data-action="publish-submit"]').trigger('click');
      expect(context.actions.publish.submit).toHaveBeenCalledOnce();
      deferred.resolve(undefined);
    });
});

async function mountDialog(
  record: ResumeRecord,
  actions: ResumeEditorActions,
): Promise<DialogHarness> {
  const existing = new Set(
    document.body.querySelectorAll('[role="dialog"]'),
  );
  const wrapper = mount(PublishDialog, {
    attachTo: document.body,
    props: { open: true, actions, record },
  });
  mounted.push(wrapper);
  await nextTick();
  await nextTick();
  const element = [...document.body.querySelectorAll('[role="dialog"]')]
    .find((candidate) => !existing.has(candidate));
  if (element === undefined) throw new Error('publish dialog is not mounted');
  return new DialogHarness(wrapper, element);
}

function editorRecord(
  metadata: Partial<ResumeRecord['accepted']['metadata']> = {},
): ResumeRecord {
  const accepted = acceptedFixture({
    metadata: { ...acceptedFixture().metadata, ...metadata },
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
  record: ResumeRecord,
  initialState: PublishControllerState = { kind: 'idle' },
): { actions: ResumeEditorActions; state: Ref<PublishControllerState> } {
  const state = ref<PublishControllerState>(initialState);
  const publish = {
    state,
    submit: vi.fn().mockResolvedValue(state.value),
    retryUncertain: vi.fn().mockResolvedValue(state.value),
    reauthPassword: vi.fn().mockResolvedValue(state.value),
    startProviderReauth: vi.fn().mockResolvedValue(state.value),
    retryAfterProviderReauth: vi.fn().mockResolvedValue(state.value),
    cancel: vi.fn(),
  };
  return {
    state,
    actions: {
      record: computed(() => record),
      createEntityId: vi.fn(() => '00000000-0000-4000-8000-000000000001'),
      edit: vi.fn(() => ({ kind: 'blocked', reason: 'not-loaded' })),
      applyTemplate: vi.fn(() => ({ kind: 'no-change' })),
      undoTemplate: vi.fn(() => ({
        kind: 'unavailable',
        reason: 'state-changed',
      })),
      recoverTemplate: vi.fn(() => ({
        kind: 'unavailable',
        reason: 'state-changed',
      })),
      resolveOpaquePhoto: vi.fn(),
      retry: vi.fn(),
      acceptLatest: vi.fn(),
      applyMine: vi.fn(),
      resumeAfterAuth: vi.fn(),
      discard: vi.fn(),
      publish: publish as never,
    },
  };
}

function deferredPromise<T>(): {
  promise: Promise<T>;
  resolve: (value: T) => void;
} {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}
