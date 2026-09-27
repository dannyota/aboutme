import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';

import AppSeal from '../../app/components/app/AppSeal.vue';
import {
  LINK_ADVANCE,
  LINK_INSET,
  linkWidth,
  sealLayout,
  STAMP_MAX_WIDTH,
  STAMP_MIN_WIDTH,
} from '../../app/components/app/sealLayout';

// AppSeal v2 (ADR 0065): a rounded ticket stamp carrying the logo's seal, the
// word PUBLIC or CÔNG KHAI, and the public link; and a 20 px seal tile mark.
describe('AppSeal', () => {
  it('renders the word, the logo seal, and the public link in the stamp',
    () => {
      const wrapper = mount(AppSeal, {
        props: { link: '/ada-lovelace', locale: 'en' },
      });

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

  it('grows with the link and squeezes only a link that cannot fit', () => {
    const width = (link: string) =>
      Number(mount(AppSeal, { props: { link, locale: 'en' } })
        .get('[data-seal-ticket]').attributes('width')) + 2.5;
    const squeeze = (link: string) =>
      mount(AppSeal, { props: { link, locale: 'en' } })
        .get('[data-seal-link]').attributes('textLength');

    // A short link leaves the ticket at the width the word needs.
    expect(width('/ada')).toBeGreaterThanOrEqual(STAMP_MIN_WIDTH);
    expect(width('/ada')).toBe(width('/bob'));
    expect(width('/nguyen-van-an-backend-engineer'))
      .toBeGreaterThan(width('/danny'));
    expect(width(`/${'w'.repeat(30)}`)).toBe(STAMP_MAX_WIDTH);
    expect(squeeze('/danny')).toBeUndefined();
    expect(Number(squeeze(`/${'w'.repeat(30)}`)))
      .toBe(STAMP_MAX_WIDTH - 2 * LINK_INSET);
  });

  it('keeps every slug inside the ticket, at its measured width', () => {
    // Any slug is `^[a-z0-9]+(-[a-z0-9]+)*$`, 4 to 30 characters; the widest
    // ones repeat the widest glyphs.
    for (const char of 'abcdefghijklmnopqrstuvwxyz0123456789') {
      for (const length of [4, 12, 20, 30]) {
        const link = `/${char.repeat(length)}`;
        const layout = sealLayout(link, 'en', -6);
        const drawn = layout.linkLength ?? linkWidth(`aboutme.vn${link}`);
        expect(drawn).toBeLessThanOrEqual(layout.width - 2 * LINK_INSET);
        expect(layout.width).toBeLessThanOrEqual(STAMP_MAX_WIDTH);
      }
    }
    for (const char of 'abcdefghijklmnopqrstuvwxyz0123456789-./') {
      expect(LINK_ADVANCE[char]).toBeGreaterThan(0);
    }
  });

  it('fits the rotated ticket inside its viewBox at any angle', () => {
    for (const rotate of [0, -6, -8, 15, -30, 90]) {
      const layout = sealLayout('/nguyen-van-an', 'vi', rotate);
      const [minX, minY, w, h] = layout.viewBox.split(' ').map(Number);
      const cx = layout.width / 2;
      const cy = 36;
      const rad = (rotate * Math.PI) / 180;
      for (const [x, y] of [
        [0, 0], [layout.width, 0], [0, 72], [layout.width, 72],
      ]) {
        const rx = cx + (x - cx) * Math.cos(rad) - (y - cy) * Math.sin(rad);
        const ry = cy + (x - cx) * Math.sin(rad) + (y - cy) * Math.cos(rad);
        expect(rx).toBeGreaterThanOrEqual(minX);
        expect(rx).toBeLessThanOrEqual(minX + w);
        expect(ry).toBeGreaterThanOrEqual(minY);
        expect(ry).toBeLessThanOrEqual(minY + h);
      }
    }
  });

  it('renders a text-free accessible mark', () => {
    const wrapper = mount(AppSeal, {
      props: { link: '/ada-lovelace', locale: 'en', size: 'mark' },
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
        locale: 'vi',
        label: 'Công khai tại aboutme.vn/ada-lovelace',
      },
    });

    expect(wrapper.get('[role="img"]').attributes('aria-label')).toBe(
      'Công khai tại aboutme.vn/ada-lovelace',
    );
  });

  it('defaults stamp rotation to minus six degrees about its center', () => {
    const wrapper = mount(AppSeal, {
      props: { link: '/ada-lovelace', locale: 'en' },
    });
    const width = Number(wrapper.get('[data-seal-ticket]').attributes('width'))
      + 2.5;

    expect(wrapper.get('[data-seal-stamp]').attributes('transform')).toBe(
      `rotate(-6 ${width / 2} 36)`,
    );
  });

  it('ignores rotation for a mark', () => {
    const wrapper = mount(AppSeal, {
      props: {
        link: '/ada-lovelace', locale: 'en', size: 'mark', rotate: 42,
      },
    });

    expect(wrapper.find('[data-seal-stamp]').exists()).toBe(false);
    expect(
      wrapper.get('[data-app-seal="mark"]').attributes('transform'),
    ).toBeUndefined();
  });

  it('renders hostile link content as text', () => {
    const link = '/<script>alert(1)</script>';
    const wrapper = mount(AppSeal, { props: { link, locale: 'en' } });

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
