import type { Customization } from '@aboutme/schema';

export type ResolvedPageGeometry = {
  marginXmm: number;
  marginYmm: number;
} & (
  | { format: 'a4'; widthPx: 794; heightPx: 1123 }
  | { format: 'letter'; widthPx: 816; heightPx: 1056 }
);

export function resolvePageGeometry(
  customization: Customization,
): ResolvedPageGeometry {
  const marginXmm = customization.spacing.pageMargin?.x ?? 15;
  const marginYmm = customization.spacing.pageMargin?.y ?? 15;
  return customization.pageFormat === 'a4'
    ? { format: 'a4', widthPx: 794, heightPx: 1123, marginXmm, marginYmm }
    : {
        format: 'letter',
        widthPx: 816,
        heightPx: 1056,
        marginXmm,
        marginYmm,
      };
}
