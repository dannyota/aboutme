import type { Resume } from '@aboutme/schema';

export const DARK_GROUND_COLORS: Resume['customization']['colors'] = {
  primary: '#a8c4ff',
  text: '#e6e8ee',
  background: '#1f2430',
  accent: '#f0b66a',
};

export function withDarkGround(document: Resume): void {
  document.customization.colors = { ...DARK_GROUND_COLORS };
}
