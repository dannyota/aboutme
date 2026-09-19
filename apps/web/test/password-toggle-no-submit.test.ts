import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { defineComponent, h, ref } from 'vue';

import IconButton from '../app/components/app/IconButton.vue';
import PasswordField from '../app/components/auth/PasswordField.vue';
import LoginPage from '../app/pages/login.vue';
import RegisterPage from '../app/pages/register.vue';
import { registerCapabilities } from './support/capabilities';
import { setSiteLocale } from './support/locale';

// An icon button inside a form must never submit it: the password
// show/hide toggle once signed people in and sent registrations.

beforeEach(() => setSiteLocale('en'));
registerCapabilities();
mockNuxtImport('navigateTo', () => vi.fn());

const settle = async (): Promise<void> => {
  await flushPromises();
  await new Promise<void>((resolve) => {
    setTimeout(resolve, 0);
  });
  await flushPromises();
};

function formWith(child: () => ReturnType<typeof h>) {
  const submit = vi.fn();
  const wrapper = mount(defineComponent({
    setup: () => () => h('form', {
      onSubmit: (event: Event) => {
        event.preventDefault();
        submit();
      },
    }, [child()]),
  }), { attachTo: document.body });
  return { submit, wrapper };
}

describe('IconButton type', () => {
  it('defaults to a top tooltip and accepts a side override', () => {
    const top = mount(IconButton, { props: { label: 'Top' } });
    expect(top.getComponent({ name: 'TooltipContent' }).props('side'))
      .toBe('top');

    const bottom = mount(IconButton, {
      props: { label: 'Bottom', tooltipSide: 'bottom' },
    });
    expect(bottom.getComponent({ name: 'TooltipContent' }).props('side'))
      .toBe('bottom');
  });

  it('defaults to a plain button and accepts an explicit submit', async () => {
    const plain = formWith(() => h(IconButton, { label: 'Plain' }));
    const button = plain.wrapper.get('button');
    expect(button.attributes('type')).toBe('button');
    await button.trigger('click');
    expect(plain.submit).not.toHaveBeenCalled();
    plain.wrapper.unmount();

    // Positive control: the same click on a submit button does submit.
    const explicit = formWith(() =>
      h(IconButton, { label: 'Send', type: 'submit' }));
    await explicit.wrapper.get('button').trigger('click');
    expect(explicit.submit).toHaveBeenCalledTimes(1);
    explicit.wrapper.unmount();
  });
});

describe('password visibility toggle', () => {
  it('shows the password without submitting the form', async () => {
    const value = ref('secret-value');
    const { submit, wrapper } = formWith(() => h(PasswordField, {
      'id': 'field',
      'label': 'Password',
      'locale': 'en',
      'modelValue': value.value,
      'onUpdate:modelValue': (next: string) => {
        value.value = next;
      },
    }));
    const toggle = wrapper.get('[aria-label="Show password"]');
    expect(toggle.attributes('type')).toBe('button');

    await toggle.trigger('click');

    expect(submit).not.toHaveBeenCalled();
    expect(wrapper.get('input').attributes('type')).toBe('text');
    wrapper.unmount();
  });

  async function clickToggleThenSubmit(
    wrapper: VueWrapper,
    endpoint: ReturnType<typeof vi.fn>,
    submitSelector: string,
  ): Promise<void> {
    await wrapper.get('[aria-label="Show password"]').trigger('click');
    await settle();
    expect(endpoint).not.toHaveBeenCalled();
    expect(wrapper.find('[aria-label="Hide password"]').exists()).toBe(true);

    // Positive control: the real submit still posts, so the page would
    // have caught a toggle submission.
    await wrapper.get(submitSelector).trigger('submit');
    await settle();
    expect(endpoint).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  }

  it('does not sign in when toggled on /login', async () => {
    const login = vi.fn(() => ({}));
    registerEndpoint('/api/v1/auth/password/login', {
      method: 'POST',
      handler: login,
    });
    // Attached: jsdom only submits a form connected to the document.
    const wrapper = await mountSuspended(LoginPage, {
      attachTo: document.body,
    });
    await wrapper.get('#login-email').setValue('ada@example.com');
    await wrapper.get('#login-password').setValue('correct horse');

    await clickToggleThenSubmit(wrapper, login, '[data-testid="login-form"]');
  });

  it('does not register when toggled on /register', async () => {
    const register = vi.fn(() => ({}));
    registerEndpoint('/api/v1/auth/password/register', {
      method: 'POST',
      handler: register,
    });
    // Attached: jsdom only submits a form connected to the document.
    const wrapper = await mountSuspended(RegisterPage, {
      attachTo: document.body,
    });
    await wrapper.get('#register-name').setValue('Ada Lovelace');
    await wrapper.get('#register-email').setValue('ada@example.com');
    await wrapper.get('#register-password')
      .setValue('correct horse battery staple');
    const confirm = wrapper.find('#register-password-confirm');
    if (confirm.exists()) {
      await confirm.setValue('correct horse battery staple');
    }

    await clickToggleThenSubmit(
      wrapper,
      register,
      '[data-testid="register-form"]',
    );
  });
});

function vueFiles(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) return entry.name === 'ui' ? [] : vueFiles(path);
    return entry.name.endsWith('.vue') ? [path] : [];
  });
}

describe('buttons in forms', () => {
  it('declares the type of every Button in a component with a form', () => {
    const untyped: string[] = [];
    for (const file of vueFiles('app')) {
      const source = readFileSync(file, 'utf8');
      const template = source.slice(source.indexOf('<template>'));
      if (!template.includes('<form')) continue;
      for (const match of template.matchAll(/<Button\b([^>]*)>/gu)) {
        const attrs = match[1] ?? '';
        if (!/(^|\s):?type=/u.test(attrs) && !/\bas-child\b/u.test(attrs)) {
          untyped.push(`${file}: ${attrs.trim().split(/\s+/u)[0]}`);
        }
      }
    }
    expect(untyped).toEqual([]);
  });
});
