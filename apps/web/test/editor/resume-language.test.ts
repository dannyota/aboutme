import { mount } from '@vue/test-utils';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { computed, defineComponent, h, nextTick, ref } from 'vue';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import EditorPreview from '../../app/components/editor/EditorPreview.vue';
import PersonalDetailsPanel from
  '../../app/components/editor/forms/PersonalDetailsPanel.vue';
import ResumeLanguageField from
  '../../app/components/editor/forms/ResumeLanguageField.vue';
import type { ResumeEditorActions } from
  '../../app/composables/useResumeEditor';
import { acceptedFixture } from './fixture';

const locale = ref<'vi' | 'en'>('en');
mockNuxtImport('useLocale', () => () => ({ locale }));

beforeEach(() => {
  locale.value = 'en';
});

type Wrapper = ReturnType<typeof mount>;

function select(wrapper: Wrapper) {
  return wrapper.get('[data-action="resume-language"]');
}

function optionLabels(wrapper: Wrapper): string[] {
  return select(wrapper)
    .findAll('option')
    .map((option) => option.text());
}

describe('ResumeLanguageField', () => {
  it('keeps resume language and emits no edit on a locale change', async () => {
    locale.value = 'en';
    const wrapper = mount(ResumeLanguageField, {
      props: { lng: 'zh-Hant' },
    });
    const select = wrapper.get('[data-action="resume-language"]');
    const emitted = wrapper.emitted('change');

    locale.value = 'vi';
    await wrapper.vm.$nextTick();

    expect((select.element as HTMLSelectElement).value).toBe('other');
    expect(
      (wrapper.get('input[name="lngOther"]').element as HTMLInputElement)
        .value,
    ).toBe('zh-Hant');
    expect(wrapper.emitted('change')).toEqual(emitted);
    expect(wrapper.text()).toContain('Ngôn ngữ CV');
  });

  it('changes a visible language-code error without changing its draft',
    async () => {
      locale.value = 'en';
      const wrapper = mount(ResumeLanguageField, {
        props: { lng: 'fr' },
        attachTo: document.body,
      });
      const code = wrapper.get('input[name="lngOther"]');
      await code.setValue('not a tag');
      await code.trigger('blur');
      (code.element as HTMLInputElement).focus();

      locale.value = 'vi';
      await wrapper.vm.$nextTick();

      expect((code.element as HTMLInputElement).value).toBe('not a tag');
      expect(document.activeElement).toBe(code.element);
      expect(wrapper.text()).toContain(
        'Nhập mã ngôn ngữ, như fr hoặc zh-Hant.',
      );
      expect(wrapper.emitted('change')).toBeUndefined();
    });

  it.each([null, 'und', ''])(
    'offers Not set for a resume with no language (%s)',
    (lng) => {
      const wrapper = mount(ResumeLanguageField, { props: { lng } });
      expect(optionLabels(wrapper)).toEqual([
        'Not set',
        'Tiếng Việt',
        'English',
        'Other…',
      ]);
      expect((select(wrapper).element as HTMLSelectElement).value).toBe(
        'unset',
      );
    },
  );

  it('saves Tiếng Việt or English as soon as it is chosen', async () => {
    const wrapper = mount(ResumeLanguageField, { props: { lng: 'vi' } });
    expect(optionLabels(wrapper)).toEqual(['Tiếng Việt', 'English', 'Other…']);

    await select(wrapper).setValue('en');
    await select(wrapper).setValue('vi');

    expect(wrapper.emitted('change')).toEqual([['en']]);
  });

  it('shows a stored tag under Other and saves a new valid one', async () => {
    const wrapper = mount(ResumeLanguageField, { props: { lng: 'fr' } });
    expect((select(wrapper).element as HTMLSelectElement).value).toBe('other');
    const code = wrapper.get('input[name="lngOther"]');
    expect((code.element as HTMLInputElement).value).toBe('fr');

    await code.setValue('not a tag');
    await code.trigger('blur');
    expect(wrapper.text()).toContain(
      'Enter a language code, such as fr or zh-Hant.',
    );
    expect(wrapper.emitted('change')).toBeUndefined();

    await code.setValue(' zh-Hant ');
    await code.trigger('keydown', { key: 'Enter' });
    expect(wrapper.emitted('change')).toEqual([['zh-Hant']]);
  });

  it('waits for a code when Other is chosen', async () => {
    const wrapper = mount(ResumeLanguageField, { props: { lng: 'vi' } });
    await select(wrapper).setValue('other');
    expect(wrapper.find('input[name="lngOther"]').exists()).toBe(true);
    expect(wrapper.emitted('change')).toBeUndefined();
  });
});

describe('resume language in the editor', () => {
  it('edits metadata lng from the Personal details panel', async () => {
    const edit = vi.fn();
    const accepted = acceptedFixture();
    const wrapper = mount(PersonalDetailsPanel, {
      props: {
        actions: {
          edit,
          createEntityId: () => 'id-1',
          record: computed(() => undefined),
        } as unknown as ResumeEditorActions,
        personal: accepted.document.personalDetails,
        lng: 'und',
      },
    });

    await wrapper.get('[data-action="resume-language"]').setValue('vi');

    expect(edit).toHaveBeenCalledWith({
      kind: 'metadataField',
      field: 'lng',
      value: 'vi',
    });
  });

  it('renders the preview in the resume language', async () => {
    const accepted = acceptedFixture();
    const contexts: { lng: string }[] = [];
    const wrapper = mount(EditorPreview, {
      props: { active: true, document: accepted.document, lng: 'vi' },
      global: {
        stubs: {
          ResumeDocument: defineComponent({
            props: { context: { type: Object, required: true } },
            setup(props) {
              return () => {
                const context = props.context as { lng: string };
                contexts.push(context);
                return h('article', { lang: context.lng });
              };
            },
          }),
        },
      },
    });
    await nextTick();
    expect(contexts.at(-1)?.lng).toBe('vi');

    await wrapper.setProps({ lng: 'zh-Hant' });
    expect(contexts.at(-1)?.lng).toBe('zh-Hant');
    wrapper.unmount();
  });
});
