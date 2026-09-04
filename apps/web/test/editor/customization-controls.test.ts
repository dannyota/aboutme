import { mount } from '@vue/test-utils';
import currentSchema from '@aboutme/schema/current-schema';
import { computed } from 'vue';
import { describe, expect, it, vi } from 'vitest';

import ColorField from
  '../../app/components/editor/customization/ColorField.vue';
import CustomizationPanel from
  '../../app/components/editor/customization/CustomizationPanel.vue';
import { CUSTOMIZATION_FIELDS } from
  '../../app/components/editor/customization/fields';
import type { ResumeEditorActions } from
  '../../app/composables/useResumeEditor';
import type { CustomizationField } from
  '../../app/components/editor/customization/fields';
import type { ResumeRecord } from '../../app/stores/resumes';
import { acceptedFixture } from './fixture';

const paths = [
  'font.family',
  'font.baseSizePx',
  'colors.primary',
  'colors.text',
  'colors.background',
  'colors.accent',
  'colors.surface',
  'spacing.sectionGap',
  'spacing.entryGap',
  'spacing.lineHeight',
  'spacing.pageMargin.x',
  'spacing.pageMargin.y',
  'heading.style',
  'heading.showRule',
  'header.align',
  'header.detailsLayout',
  'header.iconStyle',
  'layout.columns',
  'layout.surfaceTarget',
  'sectionDisplay.skill.style',
  'sectionDisplay.language.style',
  'pageFormat',
  'dateFormat',
] as const;

describe('customization fields', () => {
  it('equals the generated customization leaf set', () => {
    expect(CUSTOMIZATION_FIELDS.map(({ path }) => path)).toEqual(paths);
    expect(
      CUSTOMIZATION_FIELDS.some(({ path }) =>
        path.startsWith('layout.sections'),
      ),
    ).toBe(false);
    expect(
      CUSTOMIZATION_FIELDS.find(({ path }) => path === 'font.baseSizePx'),
    ).toMatchObject({ kind: 'integer', minimum: 10, maximum: 20 });
    expect(schemaCustomizationFields(currentSchema)).toEqual(
      CUSTOMIZATION_FIELDS.map((field) => ({
        path: field.path,
        kind: field.kind,
        required: field.required,
        values: field.values,
        minimum: field.minimum,
        maximum: field.maximum,
      })),
    );
  });
});

describe('CustomizationPanel', () => {
  it('does not emit renderer fallbacks for absent optional values', () => {
    const edit = vi.fn();
    const record = recordFor({
      colors: { primary: '#112233', text: '#223344', background: '#ffffff' },
      spacing: { sectionGap: 12, entryGap: 8, lineHeight: 1.4 },
      header: undefined,
      layout: { columns: 1, sections: { main: [], sidebar: [] } },
    });
    const wrapper = mount(CustomizationPanel, {
      props: { actions: actionsFor(edit), record },
    });

    expect(
      wrapper.get('[data-action="page-margin"]').attributes('aria-checked'),
    )
      .toBe('false');
    expect(
      wrapper.get('[data-action="header"]').attributes('aria-checked'),
    )
      .toBe('false');
    expect(
      wrapper.find('[data-field="spacing.pageMargin.x"]').exists(),
    ).toBe(false);
    expect(wrapper.find('[data-field="header.align"]').exists()).toBe(false);
    expect(wrapper.get('[data-field="colors.accent"] input').element.value)
      .toBe('#112233');
    expect(wrapper.get('[data-field="colors.surface"] input').element.value)
      .toBe('#ffffff');
    expect(
      wrapper.get('[data-field="layout.surfaceTarget"] select').element.value,
    )
      .toBe('none');
    expect(wrapper.get('[data-field="layout.surfaceTarget"] label').text())
      .toContain('Surface target');
    expect(wrapper.findAllComponents(ColorField)).toHaveLength(5);
    expect(edit).not.toHaveBeenCalled();
  });

  it('enables and removes page margins through the switch', async () => {
    const edit = vi.fn();
    const record = recordFor();
    const wrapper = mount(CustomizationPanel, {
      props: { actions: actionsFor(edit), record },
    });

    const toggle = wrapper.get('[data-action="page-margin"]');
    expect(toggle.attributes('role')).toBe('switch');
    expect(toggle.attributes('aria-checked')).toBe('false');
    await toggle.trigger('click');
    expect(edit).toHaveBeenLastCalledWith({
      kind: 'customization',
      deltas: [
        { op: 'set', path: 'spacing.pageMargin.x', value: 15 },
        { op: 'set', path: 'spacing.pageMargin.y', value: 15 },
      ],
    });
    await setCustomization(wrapper, {
      spacing: { pageMargin: { x: 15, y: 15 } },
    });
    await wrapper.get('[data-action="page-margin"]').trigger('click');
    expect(edit).toHaveBeenLastCalledWith({
      kind: 'customization',
      deltas: [{ op: 'unset', path: 'spacing.pageMargin' }],
    });
  });

  it('labels scalar fields for people, not paths', () => {
    const { wrapper } = mountPanel();
    expect(wrapper.get('[data-field="font.family"] label').text()).toBe(
      'Font',
    );
    expect(wrapper.text()).not.toContain('font.baseSizePx');
  });

  it('groups customization controls and humanizes enum options', () => {
    const wrapper = mount(CustomizationPanel, {
      props: {
        actions: actionsFor(vi.fn()),
        record: recordFor({ font: { family: 'be-vietnam-pro' } }),
      },
    });
    expect(wrapper.findAll('[data-customization-group]')).toHaveLength(5);
    expect(wrapper.find('[data-customization-group="Type"]').text())
      .toContain('Type');
    expect(wrapper.find('[data-customization-group="Spacing"]').text())
      .toContain('Spacing');
    expect(wrapper.find('[data-customization-group="Headings"]').text())
      .toContain('Headings');
    expect(wrapper.find('[data-customization-group="Layout"]').text())
      .toContain('Layout');
    expect(wrapper.find('[data-customization-group="Colors"]').text())
      .toContain('Colors');

    const family = wrapper.get('[data-field="font.family"]');
    const familySelect = family.get('[data-field-input]').element as
      HTMLSelectElement;
    expect(familySelect.selectedOptions[0]?.textContent).toBe(
      'Be Vietnam Pro',
    );
    expect(familySelect.selectedOptions[0]?.value).toBe('be-vietnam-pro');
    const barOption = Array.from(
      (wrapper.get(
        '[data-field="sectionDisplay.skill.style"] [data-field-input]',
      ).element as HTMLSelectElement).options,
    ).find((option) => option.value === 'bar');
    expect(barOption?.textContent)
      .toBe('Bar');
  });

  it('rejects an unknown enum with a linked local error', async () => {
    const edit = vi.fn();
    const wrapper = mount(CustomizationPanel, {
      props: { actions: actionsFor(edit), record: recordFor() },
    });
    const field = wrapper.get('[data-field="heading.style"]');
    const select = field.get('select');
    (select.element as HTMLSelectElement).value = 'bogus';
    await select.trigger('change');
    await wrapper.vm.$nextTick();
    const error = field.get('[data-error-for="heading.style"]');
    expect(edit).not.toHaveBeenCalled();
    expect(error.text()).toBe('Choose one of the available options.');
    expect(select.attributes('aria-describedby')).toBe(error.attributes('id'));
  });

  it(
    'uses shared buttons for issue focus and keeps optional removal a no-op',
    async () => {
      const wrapper = mount(CustomizationPanel, {
        props: {
          actions: actionsFor(vi.fn()),
          record: recordFor({}, {
            '/customization/heading/showRule': [
              { code: 'enum', path: '/customization/heading/showRule' },
            ],
          }),
        },
        attachTo: document.body,
      });
      expect(
        wrapper.get('[data-issue="/customization/heading/showRule"]')
          .attributes('data-slot'),
      ).toBe('button');
      await wrapper
        .get('[data-issue="/customization/heading/showRule"]')
        .trigger('click');
      expect(document.activeElement).toBe(
        wrapper.get('[data-field="heading.showRule"] [role="switch"]')
          .element,
      );
      const edit = vi.fn();
      await wrapper.setProps({ actions: actionsFor(edit) });
      await wrapper.get('[data-action="unset-accent"]').trigger('click');
      await wrapper.get('[data-action="unset-surface"]').trigger('click');
      expect(edit).not.toHaveBeenCalled();
      wrapper.unmount();
    },
  );

  it('preserves placement when column count changes', async () => {
    const edit = vi.fn();
    const wrapper = mount(CustomizationPanel, {
      props: {
        actions: actionsFor(edit),
        record: recordFor({ spacing: { pageMargin: { x: 15, y: 15 } } }),
      },
    });

    await wrapper.get('[data-field="layout.columns"] select').setValue('2');

    expect(edit).toHaveBeenCalledWith({
      kind: 'customization',
      deltas: [{ op: 'set', path: 'layout.columns', value: 2 }],
    });
  });

  it.each(CUSTOMIZATION_FIELDS)(
    'emits one set delta for %s',
    async (field) => {
      const edit = vi.fn();
      const record = recordWithEveryCustomizationLeaf();
      const wrapper = mount(CustomizationPanel, {
        props: { actions: actionsFor(edit), record },
      });
      const next = nextValue(field, valueAt(record, field.path));
      const control = field.kind === 'boolean'
        ? wrapper.get(
            `[data-field="${field.path}"] [role="checkbox"], `
            + `[data-field="${field.path}"] [role="switch"]`,
          )
        : wrapper.get(
            `[data-field="${field.path}"] input, `
            + `[data-field="${field.path}"] select`,
          );

      if (field.kind === 'color') {
        await control.setValue(String(next));
        await control.trigger('blur');
      } else if (field.kind === 'boolean') {
        await control.trigger('click');
      } else {
        setControlValue(control.element, next);
        await control.trigger('change');
      }

      expect(edit).toHaveBeenCalledTimes(1);
      expect(edit).toHaveBeenCalledWith({
        kind: 'customization',
        deltas: [{ op: 'set', path: field.path, value: next }],
      });
    },
  );

  it('keeps zero and rejects an out-of-bounds numeric value', async () => {
    const edit = vi.fn();
    const wrapper = mount(CustomizationPanel, {
      props: { actions: actionsFor(edit), record: recordFor() },
    });

    const input = wrapper.get('[data-field="spacing.sectionGap"] input');
    (input.element as HTMLInputElement).value = '0';
    await input.trigger('change');
    (input.element as HTMLInputElement).value = '65';
    await input.trigger('change');

    expect(edit).toHaveBeenCalledTimes(1);
    expect(edit).toHaveBeenCalledWith({
      kind: 'customization',
      deltas: [{ op: 'set', path: 'spacing.sectionGap', value: 0 }],
    });
  });

  it.each(['', 'not-a-number'])(
    'does not coerce %j into a zero section gap',
    async (invalidValue) => {
      const edit = vi.fn();
      const wrapper = mount(CustomizationPanel, {
        props: { actions: actionsFor(edit), record: recordFor() },
      });
      const input = wrapper.get('[data-field="spacing.sectionGap"] input');
      (input.element as HTMLInputElement).value = invalidValue;

      await input.trigger('change');

      expect(edit).not.toHaveBeenCalled();
    },
  );

  it('sets and unsets the optional surface target directly', async () => {
    const edit = vi.fn();
    const record = recordFor();
    record.current.document.customization.layout.surfaceTarget = 'header';
    const wrapper = mount(CustomizationPanel, {
      props: { actions: actionsFor(edit), record },
    });

    await wrapper
      .get('[data-field="layout.surfaceTarget"] select')
      .setValue('sidebar');
    await wrapper.get('[data-action="unset-surface-target"]').trigger('click');

    expect(edit).toHaveBeenNthCalledWith(1, {
      kind: 'customization',
      deltas: [{ op: 'set', path: 'layout.surfaceTarget', value: 'sidebar' }],
    });
    expect(edit).toHaveBeenNthCalledWith(2, {
      kind: 'customization',
      deltas: [{ op: 'unset', path: 'layout.surfaceTarget' }],
    });
  });

  it('rejects an invalid color without changing the preview', async () => {
    const edit = vi.fn();
    const wrapper = mount(ColorField, {
      props: { label: 'Primary color', modelValue: '#112233' },
    });

    await wrapper.get('input').setValue('not-a-color');
    await wrapper.get('input').trigger('blur');

    expect(wrapper.get('[role="alert"]').text()).toBe(
      'Enter a six-digit hex color.',
    );
    expect(wrapper.get('input').attributes('aria-invalid')).toBe('true');
    expect(wrapper.get('input').attributes('aria-describedby')).toBe(
      wrapper.get('[role="alert"]').attributes('id'),
    );
    expect(wrapper.emitted('set')).toBeUndefined();
    expect(edit).not.toHaveBeenCalled();
  });

  it(
    'removes a dirty optional color with exactly one unset command',
    async () => {
      const edit = vi.fn();
      const record = recordFor();
      record.current.document.customization.colors.accent = '#abcdef';
      const wrapper = mount(CustomizationPanel, {
        props: { actions: actionsFor(edit), record },
      });
      const input = wrapper.get('[data-field="colors.accent"] input');
      const remove = wrapper.get('[data-action="unset-accent"]');

      await input.setValue('#123456');
      const pointer = new MouseEvent('mousedown', {
        bubbles: true,
        cancelable: true,
      });
      remove.element.dispatchEvent(pointer);
      expect(pointer.defaultPrevented).toBe(true);
      await input.trigger('blur', { relatedTarget: remove.element });
      await remove.trigger('click');

      expect(edit).toHaveBeenCalledTimes(1);
      expect(edit).toHaveBeenCalledWith({
        kind: 'customization',
        deltas: [{ op: 'unset', path: 'colors.accent' }],
      });
    },
  );

  it(
    'removes a dirty optional color after keyboard focus moves to Remove',
    async () => {
      const edit = vi.fn();
      const record = recordFor();
      record.current.document.customization.colors.accent = '#abcdef';
      const wrapper = mount(CustomizationPanel, {
        props: { actions: actionsFor(edit), record },
      });
      const input = wrapper.get('[data-field="colors.accent"] input');
      const remove = wrapper.get('[data-action="unset-accent"]');

      await input.setValue('#123456');
      await input.trigger('blur', { relatedTarget: remove.element });
      await remove.trigger('click', { detail: 0 });

      expect(edit).toHaveBeenCalledTimes(1);
      expect(edit).toHaveBeenCalledWith({
        kind: 'customization',
        deltas: [{ op: 'unset', path: 'colors.accent' }],
      });
    },
  );

  it('associates every local error with the invalid input', async () => {
    const edit = vi.fn();
    const wrapper = mount(CustomizationPanel, {
      props: {
        actions: actionsFor(edit),
        record: recordFor({ spacing: { pageMargin: { x: 15, y: 15 } } }),
      },
    });
    const gap = wrapper.get('[data-field="spacing.sectionGap"] input');
    const margin = wrapper.get('[data-field="spacing.pageMargin.x"] input');

    setControlValue(gap.element, 65);
    await gap.trigger('change');
    setControlValue(margin.element, 41);
    await margin.trigger('change');

    for (const path of ['spacing.sectionGap', 'spacing.pageMargin.x']) {
      const input = wrapper.get(`[data-field="${path}"] input`);
      const error = wrapper.get(`[data-error-for="${path}"]`);
      expect(input.attributes('aria-invalid')).toBe('true');
      expect(input.attributes('aria-describedby')).toBe(error.attributes('id'));
      expect(error.text()).toBe('Enter a value within the allowed range.');
    }
    expect(edit).not.toHaveBeenCalled();
  });

  it.each([
    ['page-margin', false],
    ['page-margin', true],
    ['header', false],
    ['header', true],
    ['unset-accent', true],
    ['unset-surface', true],
    ['unset-surface-target', true],
  ] as const)('accepts a keyboard click for %s', async (action, present) => {
    const edit = vi.fn();
    const record = present ? recordWithEveryCustomizationLeaf() : recordFor();
    const wrapper = mount(CustomizationPanel, {
      props: { actions: actionsFor(edit), record },
    });
    const button = wrapper.get(`[data-action="${action}"]`);

    await button.trigger('click', { detail: 0 });

    expect(edit).toHaveBeenCalledTimes(1);
  });

  it.each([
    'header.align',
    'header.detailsLayout',
    'header.iconStyle',
    'layout.surfaceTarget',
  ] as const)('uses generated enum values for %s', (path) => {
    const wrapper = mount(CustomizationPanel, {
      props: {
        actions: actionsFor(vi.fn()),
        record: recordWithEveryCustomizationLeaf(),
      },
    });
    const options = wrapper
      .findAll(`[data-field="${path}"] option`)
      .map((option) => option.element.getAttribute('value'));
    const field = CUSTOMIZATION_FIELDS.find((item) => item.path === path)!;

    expect(options).toEqual(field.values);
  });

  it('uses a safe issue message and focuses the known field', async () => {
    const edit = vi.fn();
    const record = recordFor({}, {
      '/customization/colors/primary': [
        { code: 'pattern', path: '/customization/colors/primary' },
      ],
    });
    const wrapper = mount(CustomizationPanel, {
      attachTo: document.body,
      props: { actions: actionsFor(edit), record },
    });

    const issue = wrapper.get('[data-issue="/customization/colors/primary"]');
    expect(issue.text()).toBe('Enter a value in the required format.');
    await issue.trigger('click');

    expect(document.activeElement).toBe(
      wrapper.get('[data-field="colors.primary"] input').element,
    );
    wrapper.unmount();
  });
});

function recordFor(
  customization: Partial<
    ResumeRecord['current']['document']['customization']
  > = {},
  issues: ResumeRecord['issues'] = {},
): ResumeRecord {
  const current = acceptedFixture();
  current.document.customization = {
    ...current.document.customization,
    ...customization,
  };
  return {
    current,
    accepted: current,
    issues,
    pending: [],
    conflicts: [],
    sessionLost: false,
  } as ResumeRecord;
}

function mountPanel() {
  const edit = vi.fn();
  const wrapper = mount(CustomizationPanel, {
    props: { actions: actionsFor(edit), record: recordFor() },
  });
  return { edit, wrapper };
}

async function setCustomization(
  wrapper: ReturnType<typeof mount>,
  patch: Record<string, unknown>,
): Promise<void> {
  const record = wrapper.props('record') as ResumeRecord;
  const customization = record.current.document.customization;
  record.current.document.customization = {
    ...customization,
    ...patch,
    spacing: {
      ...customization.spacing,
      ...(patch.spacing as object | undefined),
    },
  };
  await wrapper.setProps({ record });
}

function actionsFor(edit: ReturnType<typeof vi.fn>): ResumeEditorActions {
  return {
    record: computed(() => undefined),
    edit,
  } as unknown as ResumeEditorActions;
}

function schemaCustomizationFields(schema: unknown): readonly {
  path: string;
  kind: CustomizationField['kind'];
  required: boolean;
  values?: readonly (string | number)[];
  minimum?: number;
  maximum?: number;
}[] {
  const root = schema as {
    properties: { customization: { $ref: string } };
    $defs: Record<string, SchemaNode>;
  };
  const definition = resolve(root, root.properties.customization)!;
  return walk(definition, root, '', true).filter(
    (field) => !field.path.startsWith('layout.sections'),
  );
}

function walk(
  raw: SchemaNode,
  root: { $defs: Record<string, SchemaNode> },
  prefix: string,
  required: boolean,
): {
  path: string;
  kind: CustomizationField['kind'];
  required: boolean;
  values?: readonly (string | number)[];
  minimum?: number;
  maximum?: number;
}[] {
  const node = resolve(root, raw)!;
  if (node.properties === undefined) {
    return [{
      path: prefix,
      kind: schemaKind(node),
      required,
      ...(node.enum === undefined ? {} : { values: node.enum }),
      ...(node.minimum === undefined ? {} : { minimum: node.minimum }),
      ...(node.maximum === undefined ? {} : { maximum: node.maximum }),
    }];
  }
  return Object.entries(node.properties).flatMap(([name, child]) => {
    const path = prefix === '' ? name : `${prefix}.${name}`;
    if (path === 'layout.sections') return [];
    return walk(
      child,
      root,
      path,
      required && (node.required?.includes(name) ?? false),
    );
  });
}

type SchemaNode = {
  $ref?: string;
  enum?: readonly (string | number)[];
  maximum?: number;
  minimum?: number;
  pattern?: string;
  properties?: Record<string, SchemaNode>;
  required?: readonly string[];
  type?: 'boolean' | 'integer' | 'number' | 'object' | 'string';
};

function resolve(
  root: { $defs: Record<string, SchemaNode> },
  node: SchemaNode,
): SchemaNode | undefined {
  if (node.$ref === undefined) return node;
  return root.$defs[node.$ref.slice('#/$defs/'.length)];
}

function schemaKind(node: SchemaNode): CustomizationField['kind'] {
  if (node.pattern === '^#[0-9a-fA-F]{6}$') return 'color';
  if (node.enum !== undefined) return 'enum';
  if (node.type === 'boolean' || node.type === 'integer'
    || node.type === 'number') {
    return node.type;
  }
  throw new Error(`unsupported schema leaf ${node.type}`);
}

function recordWithEveryCustomizationLeaf(): ResumeRecord {
  const record = recordFor();
  record.current.document.customization.colors.accent = '#abcdef';
  record.current.document.customization.colors.surface = '#fedcba';
  record.current.document.customization.spacing.pageMargin = {
    x: 15,
    y: 15,
  };
  record.current.document.customization.header = {
    align: 'left',
    detailsLayout: 'inline',
    iconStyle: 'outline',
  };
  record.current.document.customization.layout.surfaceTarget = 'header';
  return record;
}

function valueAt(
  record: ResumeRecord,
  path: string,
): string | number | boolean {
  let value: unknown = record.current.document.customization;
  for (const part of path.split('.')) {
    value = (value as Record<string, unknown>)[part];
  }
  if (typeof value === 'string' || typeof value === 'number'
    || typeof value === 'boolean') {
    return value;
  }
  throw new Error(`missing fixture value for ${path}`);
}

function nextValue(
  field: CustomizationField,
  current: string | number | boolean,
): string | number | boolean {
  if (field.kind === 'color') {
    return current === '#123456' ? '#654321' : '#123456';
  }
  if (field.kind === 'boolean') return !current;
  if (field.kind === 'enum') {
    return field.values!.find((value) => value !== current)!;
  }
  return field.minimum === undefined ? 0 : field.minimum;
}

function setControlValue(
  element: Element,
  value: string | number | boolean,
): void {
  if (element instanceof HTMLInputElement) {
    if (element.type === 'checkbox') element.checked = value === true;
    else element.value = String(value);
    return;
  }
  if (element instanceof HTMLSelectElement) {
    element.value = String(value);
    return;
  }
  throw new Error('expected an input or select control');
}
