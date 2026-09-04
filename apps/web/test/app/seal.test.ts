import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';

import AppSeal from '../../app/components/app/AppSeal.vue';

describe('AppSeal', () => {
  it('renders the public link in the stamp ring and accessible name', () => {
    const wrapper = mount(AppSeal, { props: { link: '/ada-lovelace' } });

    expect(wrapper.get('[role="img"]').attributes('aria-label')).toBe(
      'Public at aboutme.vn/ada-lovelace',
    );
    expect(wrapper.get('[data-seal-ring-text]').element.textContent).toBe(
      'PUBLIC RESUME · ABOUTME.VN/ADA-LOVELACE · ',
    );
    expect(wrapper.get('[data-seal-ring-label]').attributes('font-size')).toBe(
      '9',
    );
    expect(
      wrapper.get('[data-seal-ring-label]').attributes('letter-spacing'),
    ).toBe('0.08em');
    expect(wrapper.get('[data-seal-ring="outer"]').attributes()).toMatchObject({
      'r': '45',
      'stroke-width': '2',
    });
    expect(wrapper.get('[data-seal-ring="inner"]').attributes()).toMatchObject({
      'r': '39',
      'stroke-width': '1',
    });
    expect(wrapper.get('[data-seal-center]').text()).toBe('aboutme');
    expect(wrapper.get('[data-seal-center]').attributes()).toMatchObject({
      'font-size': '14',
      'font-weight': '600',
    });
    expect(wrapper.attributes('style')).toBe('color: var(--seal);');
  });

  it('renders a text-free accessible mark', () => {
    const wrapper = mount(AppSeal, {
      props: { link: '/ada-lovelace', size: 'mark' },
    });

    expect(wrapper.get('[role="img"]').attributes('aria-label')).toBe(
      'Public at aboutme.vn/ada-lovelace',
    );
    expect(wrapper.get('[data-app-seal="mark"]').text()).toBe('');
    expect(wrapper.find('[data-seal-ring-text]').exists()).toBe(false);
    expect(wrapper.get('[data-seal-check]').exists()).toBe(true);
  });

  it('defaults stamp rotation to minus eight degrees', () => {
    const wrapper = mount(AppSeal, { props: { link: '/ada-lovelace' } });

    expect(wrapper.get('[data-seal-stamp]').attributes('transform')).toBe(
      'rotate(-8 48 48)',
    );
  });

  it('ignores rotation for a mark', () => {
    const wrapper = mount(AppSeal, {
      props: { link: '/ada-lovelace', size: 'mark', rotate: 42 },
    });

    expect(wrapper.find('[data-seal-stamp]').exists()).toBe(false);
    expect(
      wrapper.get('[data-app-seal="mark"]').attributes('transform'),
    ).toBeUndefined();
  });

  it('renders hostile link content as text', () => {
    const link = '/<script>alert(1)</script>';
    const wrapper = mount(AppSeal, { props: { link } });

    expect(wrapper.get('[data-seal-ring-text]').text()).toContain(
      '<SCRIPT>ALERT(1)</SCRIPT>',
    );
    expect(wrapper.attributes('aria-label')).toBe(
      `Public at aboutme.vn${link}`,
    );
    expect(descendantNames(wrapper.element)).not.toContain('script');
  });
});

function descendantNames(root: Element): string[] {
  const names: string[] = [];
  const pending = [...root.children];
  while (pending.length > 0) {
    const element = pending.pop()!;
    names.push(element.localName);
    pending.push(...element.children);
  }
  return names;
}
