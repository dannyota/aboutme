import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';

import AppSeal from '../../app/components/app/AppSeal.vue';

// AppSeal v2 (ADR 0065): a rounded ticket stamp carrying the logo's seal, the
// word PUBLIC or CÔNG KHAI, and the public link; and a 20 px seal tile mark.
describe('AppSeal', () => {
  it('renders the word, the logo seal, and the public link in the stamp',
    () => {
      const wrapper = mount(AppSeal, { props: { link: '/ada-lovelace' } });

      expect(wrapper.get('[role="img"]').attributes('aria-label')).toBe(
        'Public at aboutme.vn/ada-lovelace',
      );
      expect(wrapper.get('[data-seal-word]').text()).toBe('PUBLIC');
      expect(wrapper.get('[data-seal-word]').attributes()).toMatchObject({
        'font-size': '20',
        'font-weight': '800',
      });
      expect(wrapper.get('[data-seal-link]').text()).toBe(
        'aboutme.vn/ada-lovelace',
      );
      expect(wrapper.find('[data-seal-logo]').exists()).toBe(true);
      expect(wrapper.get('[data-seal-ticket]').attributes()).toMatchObject({
        'rx': '14',
        'stroke-width': '2.5',
      });
      expect(wrapper.attributes('class')).toContain('text-seal-text');
    });

  it('says CÔNG KHAI for a Vietnamese stamp', () => {
    const wrapper = mount(AppSeal, {
      props: { link: '/ada-lovelace', locale: 'vi' },
    });

    expect(wrapper.get('[data-seal-word]').text()).toBe('CÔNG KHAI');
  });

  it('grows with the link and never lets a long slug overflow', () => {
    const short = mount(AppSeal, { props: { link: '/ada' } });
    const long = mount(AppSeal, {
      props: { link: '/nguyen-van-an-backend-engineer' },
    });
    const longest = mount(AppSeal, { props: { link: `/${'a'.repeat(30)}` } });
    const width = (w: ReturnType<typeof mount>) =>
      Number(w.get('[data-seal-ticket]').attributes('width'));

    expect(width(short)).toBeLessThan(width(long));
    expect(width(longest)).toBeLessThanOrEqual(260);
    expect(short.get('[data-seal-link]').attributes('textLength'))
      .toBeUndefined();
    const squeezed = Number(
      longest.get('[data-seal-link]').attributes('textLength'),
    );
    expect(squeezed).toBeGreaterThan(0);
    expect(squeezed).toBeLessThanOrEqual(width(longest) - 40 + 2.5);
  });

  it('renders a text-free accessible mark', () => {
    const wrapper = mount(AppSeal, {
      props: { link: '/ada-lovelace', size: 'mark' },
    });

    expect(wrapper.get('[role="img"]').attributes('aria-label')).toBe(
      'Public at aboutme.vn/ada-lovelace',
    );
    expect(wrapper.get('[data-app-seal="mark"]').text()).toBe('');
    expect(wrapper.find('[data-seal-word]').exists()).toBe(false);
    expect(wrapper.get('[data-seal-check]').exists()).toBe(true);
    expect(wrapper.get('rect').attributes('rx')).toBe('6');
  });

  it('uses a caller-supplied accessible label', () => {
    const wrapper = mount(AppSeal, {
      props: {
        link: '/ada-lovelace',
        label: 'Công khai tại aboutme.vn/ada-lovelace',
      },
    });

    expect(wrapper.get('[role="img"]').attributes('aria-label')).toBe(
      'Công khai tại aboutme.vn/ada-lovelace',
    );
  });

  it('defaults stamp rotation to minus six degrees about its center', () => {
    const wrapper = mount(AppSeal, { props: { link: '/ada-lovelace' } });
    const width = Number(wrapper.get('[data-seal-ticket]').attributes('width'))
      + 2.5;

    expect(wrapper.get('[data-seal-stamp]').attributes('transform')).toBe(
      `rotate(-6 ${width / 2} 36)`,
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

    expect(wrapper.get('[data-seal-link]').text()).toContain(
      '<script>alert(1)</script>',
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
