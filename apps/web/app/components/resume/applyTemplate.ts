import type { Content, Customization } from '@aboutme/schema';
import type {
  SectionType,
  TemplatePreset,
} from '@aboutme/schema/templates';

export type TemplateApplyErrorCode
  = | 'invalid_current_placement'
    | 'invalid_preset_placement';

export class TemplateApplyError extends Error {
  readonly code: TemplateApplyErrorCode;

  constructor(code: TemplateApplyErrorCode, message: string) {
    super(message);
    this.name = 'TemplateApplyError';
    this.code = code;
  }
}

function currentVisualOrder(
  current: Customization,
  content: Content,
): string[] {
  const visualOrder = [
    ...current.layout.sections.main,
    ...current.layout.sections.sidebar,
  ];
  const contentKeys = Object.keys(content);
  const uniqueKeys = new Set(visualOrder);
  if (
    uniqueKeys.size !== visualOrder.length
    || uniqueKeys.size !== contentKeys.length
    || contentKeys.some((key) => !uniqueKeys.has(key))
  ) {
    throw new TemplateApplyError(
      'invalid_current_placement',
      'Current placement must contain every content key exactly once.',
    );
  }
  return visualOrder;
}

function validateSelectors(preset: TemplatePreset): readonly SectionType[] {
  const { placement, sidebarSectionTypes } = preset.customization.layout;
  if (placement === 'keep') {
    if (sidebarSectionTypes !== undefined) {
      throw new TemplateApplyError(
        'invalid_preset_placement',
        'A keep preset cannot define sidebar section selectors.',
      );
    }
    return [];
  }

  if (placement !== 'byType') {
    throw new TemplateApplyError(
      'invalid_preset_placement',
      'A preset placement must be keep or byType.',
    );
  }

  if (sidebarSectionTypes === undefined) {
    throw new TemplateApplyError(
      'invalid_preset_placement',
      'A byType preset must define sidebar section selectors.',
    );
  }
  const unique = new Set(sidebarSectionTypes);
  if (
    unique.size !== sidebarSectionTypes.length
    || sidebarSectionTypes.includes('custom')
  ) {
    throw new TemplateApplyError(
      'invalid_preset_placement',
      'Sidebar section selectors must be unique and cannot include custom.',
    );
  }
  return sidebarSectionTypes;
}

export function applyTemplate(
  current: Customization,
  preset: TemplatePreset,
  content: Content,
): Customization {
  const visualOrder = currentVisualOrder(current, content);
  const selectors = validateSelectors(preset);
  const presetCustomization = structuredClone(preset.customization);
  const { placement, sidebarSectionTypes: _selectors, ...presetLayout }
    = presetCustomization.layout;

  if (placement === 'keep') {
    return {
      ...presetCustomization,
      layout: {
        ...presetLayout,
        sections: structuredClone(current.layout.sections),
      },
    };
  }

  const sidebar: string[] = [];
  for (const selector of selectors) {
    for (const key of visualOrder) {
      if (content[key]?.sectionType === selector) {
        sidebar.push(key);
      }
    }
  }
  const sidebarKeys = new Set(sidebar);
  const main = visualOrder.filter((key) => !sidebarKeys.has(key));

  return {
    ...presetCustomization,
    layout: { ...presetLayout, sections: { main, sidebar } },
  };
}
