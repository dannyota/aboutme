import type { Customization } from '@aboutme/schema';
import { TEMPLATES } from '@aboutme/schema/templates';
import { describe, expect, it } from 'vitest';

import {
  contrastRatio,
  mixInSRGB,
} from '../../app/components/resume/clampContrast';
import {
  effectiveSurfaceTarget,
  useResumeStyles,
} from '../../app/components/resume/useResumeStyles';

const storedCustomization = (
  preset: (typeof TEMPLATES)[number],
): Customization => {
  const { placement: _placement, sidebarSectionTypes: _types, ...layout }
    = preset.customization.layout;
  return {
    ...structuredClone(preset.customization),
    layout: { ...layout, sections: { main: [], sidebar: [] } },
  };
};

const expectAtLeast = (
  color: string,
  surface: string,
  target: number,
  label: string,
): void => {
  expect(contrastRatio(color, surface), label).toBeGreaterThanOrEqual(target);
};

describe('preset contrast conformance', () => {
  it('passes each raw page palette in the required sRGB mix space', () => {
    for (const preset of TEMPLATES) {
      const colors = preset.customization.colors;
      const surface = colors.background;
      const accent = colors.accent ?? colors.primary;
      const track = mixInSRGB(accent, surface, 0.8);
      expectAtLeast(colors.primary, surface, 4.5, `${preset.id} heading`);
      expectAtLeast(colors.text, surface, 4.5, `${preset.id} body`);
      expectAtLeast(
        mixInSRGB(colors.text, surface, 0.25),
        surface,
        4.5,
        `${preset.id} meta`,
      );
      expectAtLeast(accent, surface, 4.5, `${preset.id} accent text`);
      expectAtLeast(
        mixInSRGB(accent, surface, 0.6),
        surface,
        1.5,
        `${preset.id} rule`,
      );
      expectAtLeast(accent, surface, 3, `${preset.id} level surface`);
      expectAtLeast(accent, track, 3, `${preset.id} level track`);
    }
  });

  it('passes every runtime role on every actual surface after clamping', () => {
    for (const preset of TEMPLATES) {
      const customization = storedCustomization(preset);
      const styles = useResumeStyles(customization);
      const target = effectiveSurfaceTarget(customization);
      const scopes = [
        [`${preset.id} page`, styles.root],
        ...(target === 'header' && styles.header
          ? [[`${preset.id} header`, styles.header] as const]
          : []),
        ...(target === 'sidebar' && styles.sidebar
          ? [[`${preset.id} sidebar`, styles.sidebar] as const]
          : []),
      ] as const;
      for (const [label, scope] of scopes) {
        const surface = scope['--color-surface']!;
        for (const role of [
          '--color-heading',
          '--color-body',
          '--color-meta',
          '--color-accent-text',
          '--color-link',
        ]) {
          expectAtLeast(scope[role]!, surface, 4.5, `${label} ${role}`);
        }
        expectAtLeast(scope['--color-rule']!, surface, 1.5, `${label} rule`);
        const solid = scope['--color-accent-solid']!;
        const track = scope['--color-track']!;
        expectAtLeast(solid, surface, 3, `${label} level surface`);
        expectAtLeast(solid, track, 3, `${label} level track`);
        expectAtLeast(
          scope['--color-on-accent']!,
          solid,
          4.5,
          `${label} on accent`,
        );
      }
    }
  });
});
