import { parse as parseTemplate } from '@vue/compiler-dom';
import { parse as parseSfc } from '@vue/compiler-sfc';
import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative, resolve } from 'node:path';
import ts from 'typescript';
import { describe, expect, test } from 'vitest';

import { editorControlsCopy } from '../../app/i18n/editor-controls';
import { editorFieldsCopy } from '../../app/i18n/editor-fields';
import { editorSectionsCopy } from '../../app/i18n/editor-sections';
import { editorShellCopy } from '../../app/i18n/editor-shell';
import { workspaceTitles } from '../../app/i18n/meta';
import { pdfCopy } from '../../app/i18n/pdf';
import { publishCopy } from '../../app/i18n/publish';
import { resumeCreateCopy } from '../../app/i18n/resume-create';
import { resumeListCopy } from '../../app/i18n/resume-list';
import { shellCopy } from '../../app/i18n/shell';
import { workspaceCopy } from '../../app/i18n/workspace';

const appRoot = resolve(process.cwd(), 'app');
const displayKeys = new Set([
  'ariaLabel',
  'description',
  'help',
  'hint',
  'label',
  'message',
  'placeholder',
  'text',
  'title',
]);
const guardedAttributes = new Set([
  'aria-label',
  'cancel-label',
  'close-label',
  'confirm-label',
  'description',
  'label',
  'placeholder',
  'title',
]);
const catalogs = [
  'i18n/editor-controls.ts',
  'i18n/editor-fields.ts',
  'i18n/editor-sections.ts',
  'i18n/editor-shell.ts',
  'i18n/meta.ts',
  'i18n/pdf.ts',
  'i18n/publish.ts',
  'i18n/resume-create.ts',
  'i18n/resume-list.ts',
  'i18n/shell.ts',
  'i18n/workspace.ts',
];
const vueSources = [
  'components/app/AccountMenu.vue',
  'components/app/AppSeal.vue',
  'components/app/AppShell.vue',
  'components/app/LocaleToggle.vue',
  'components/app/StateMark.vue',
  'components/editor/ConflictPanel.vue',
  'components/editor/EditorPreview.vue',
  'components/editor/EditorShell.vue',
  'components/editor/EntryCard.vue',
  'components/editor/ErrorSummary.vue',
  'components/editor/PDFDownloadButton.vue',
  'components/editor/PreviewToolbar.vue',
  'components/editor/PublishDialog.vue',
  'components/editor/PublishPageFields.vue',
  'components/editor/SaveStatus.vue',
  'components/editor/customization/ColorField.vue',
  'components/editor/customization/CustomizationPanel.vue',
  'components/editor/customization/PageSettings.vue',
  'components/editor/forms/ContactList.vue',
  'components/editor/forms/DateRangeField.vue',
  'components/editor/forms/PersonalDetailsPanel.vue',
  'components/editor/forms/ResumeLanguageField.vue',
  'components/editor/forms/SectionPanel.vue',
  'components/editor/forms/YearMonthField.vue',
  'components/editor/forms/entries/CertificateEntryFields.vue',
  'components/editor/forms/entries/CustomEntryFields.vue',
  'components/editor/forms/entries/EducationEntryFields.vue',
  'components/editor/forms/entries/EntryLinkField.vue',
  'components/editor/forms/entries/LanguageEntryFields.vue',
  'components/editor/forms/entries/ProfileEntryFields.vue',
  'components/editor/forms/entries/ProjectEntryFields.vue',
  'components/editor/forms/entries/SkillEntryFields.vue',
  'components/editor/forms/entries/WorkEntryFields.vue',
  'components/editor/list/CreateResumeDialog.vue',
  'components/editor/list/DeleteResumeDialog.vue',
  'components/editor/list/RenameResumeDialog.vue',
  'components/editor/list/ResumeList.vue',
  'components/editor/photo/CropEditor.vue',
  'components/editor/photo/PhotoPanel.vue',
  'components/editor/richtext/RichTextEditor.vue',
  'components/editor/structure/EntryOrderControls.vue',
  'components/editor/structure/SectionControls.vue',
  'components/editor/structure/StructurePanel.vue',
  'components/editor/templates/TemplatePanel.vue',
  'components/editor/templates/TemplatePartialDialog.vue',
  'components/editor/templates/TemplateThumbnail.vue',
  'pages/app/new.vue',
  'pages/app/resumes/[id].vue',
  'pages/app/resumes/index.vue',
];
const helperSources = [
  'components/editor/customization/labels.ts',
  'components/editor/customization/pageSettings.ts',
  'components/editor/forms/entries/levels.ts',
  'components/editor/resumeLanguage.ts',
  'components/editor/sectionTypes.ts',
  'composables/useResumeList.ts',
  'editor/pdfDownload.ts',
  'utils/relativeTime.ts',
];
const approvedLiterals: Readonly<Record<string, readonly string[]>> = {
  'components/app/AppSeal.vue': ['Public at aboutme.vn', 'aboutme'],
  'components/app/AppShell.vue': ['aboutme'],
  'components/app/StateMark.vue': ['aboutme.vn'],
  'components/editor/EditorShell.vue': ['aboutme', '+'],
  'components/editor/PreviewToolbar.vue': [
    '100%',
    String.fromCodePoint(0x2014),
  ],
  'components/editor/PublishDialog.vue': ['aboutme.vn/', 'aboutme.vn'],
  'components/editor/photo/CropEditor.vue': ['X', 'Y'],
  'components/editor/templates/TemplatePartialDialog.vue': ['.'],
  'pages/app/new.vue': ['.'],
  'components/editor/resumeLanguage.ts': ['Tiếng Việt', 'English'],
  'components/editor/customization/pageSettings.ts': [' · '],
  'components/editor/forms/entries/levels.ts': [' · '],
  'components/editor/sectionTypes.ts': [
    'Summary',
    'Experience',
    'Education',
    'Skills',
    'Languages',
    'Certifications',
    'Projects',
    'Custom section',
  ],
};
const functionFixtures: Readonly<Record<string, readonly unknown[]>> = {
  'editor-controls.controls.deleteSectionDescription': ['Summary'],
  'editor-controls.controls.movedToMain': ['Summary'],
  'editor-controls.controls.movedToSidebar': ['Summary'],
  'editor-controls.controls.photoStatusBusy': ['Wait'],
  'editor-controls.controls.photoStatusRateLimited': ['Wait'],
  'editor-controls.controls.tryAgainLater': [2],
  'editor-controls.page.horizontal': ['mm'],
  'editor-controls.page.vertical': ['mm'],
  'editor-controls.page.edge': ['10 mm'],
  'editor-controls.page.invalid': ['99'],
  'editor-controls.page.preset': ['A4', '210 × 297 mm'],
  'editor-fields.section.deleteDescription': ['Entry'],
  'editor-sections.entry': [1],
  'editor-shell.conflictControl': ['apply-field'],
  'editor-shell.pageCount': [1],
  'editor-shell.issueFor': ['required'],
  'pdf.ariaDownload': ['A4'],
  'publish.page.titleHint': [1, 2],
  'publish.page.useIcon': ['✓'],
  'resume-create.blankSummary': ['Template'],
  'resume-create.sampleSummary': ['Template', 'Role', 'English'],
  'resume-create.cap': [3],
  'resume-create.count': [1, 3],
  'resume-create.browseAll': [3],
  'resume-create.languageName': ['en'],
  'resume-list.updated': ['now'],
  'resume-list.moreActions': ['Resume'],
  'resume-list.rename': ['Resume'],
  'resume-list.delete': ['Resume'],
  'resume-list.deleteDescription': ['Resume'],
  'resume-list.relativeTime.minutes': [2],
  'resume-list.relativeTime.hours': [2],
  'resume-list.relativeTime.days': [2],
  'resume-list.relativeTime.date': [1, 2, 2026],
  'workspace.publicAt': ['/resume'],
};
const workspaceCatalogNames = new Set(
  catalogs.map((path) => path.slice(5, -3)),
);

type TemplateProp = {
  readonly type: number;
  readonly name: string;
  readonly value?: { readonly content: string };
  readonly arg?: { readonly content: string };
  readonly exp?: { readonly content: string };
};
type TemplateNode = {
  readonly type: number;
  readonly content?: string;
  readonly props?: readonly TemplateProp[];
  readonly children?: readonly TemplateNode[];
  readonly branches?: readonly TemplateNode[];
};

function source(path: string): string {
  return readFileSync(join(appRoot, path), 'utf8');
}

function literalTexts(node: ts.Node): readonly string[] {
  if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) {
    return node.text === '' ? [] : [node.text];
  }
  if (ts.isTemplateExpression(node)) {
    return [
      node.head.text,
      ...node.templateSpans.map((span) => span.literal.text),
    ]
      .filter((text) => text.trim() !== '');
  }
  return [];
}

function propertyName(
  name: ts.PropertyName | ts.BindingName,
): string | undefined {
  if (ts.isIdentifier(name) || ts.isStringLiteral(name)) return name.text;
  return undefined;
}

function allowed(path: string, text: string): boolean {
  return approvedLiterals[path]?.includes(text) ?? false;
}

function expressionLiterals(input: string): readonly string[] {
  const file = ts.createSourceFile(
    'template.ts',
    input,
    ts.ScriptTarget.Latest,
  );
  const literals: string[] = [];
  const expression = file.statements[0];
  if (expression === undefined || !ts.isExpressionStatement(expression)) {
    return literals;
  }
  const walk = (node: ts.Expression): void => {
    literals.push(...literalTexts(node));
    if (ts.isParenthesizedExpression(node)) {
      walk(node.expression);
    } else if (ts.isConditionalExpression(node)) {
      walk(node.whenTrue);
      walk(node.whenFalse);
    } else if (
      ts.isBinaryExpression(node)
      && [
        ts.SyntaxKind.PlusToken,
        ts.SyntaxKind.QuestionQuestionToken,
        ts.SyntaxKind.BarBarToken,
      ].includes(node.operatorToken.kind)
    ) {
      walk(node.left);
      walk(node.right);
    }
  };
  walk(expression.expression);
  return literals;
}

function templateViolations(path: string, input: string): string[] {
  const parsed = parseTemplate(input, { comments: false });
  const violations: string[] = [];
  const walk = (node: TemplateNode): void => {
    if (
      node.type === 2
      && node.content.trim() !== ''
      && !allowed(path, node.content.trim())
    ) {
      violations.push(`static text ${JSON.stringify(node.content.trim())}`);
    }
    if (node.type === 1) {
      for (const prop of node.props ?? []) {
        if (
          prop.type === 6
          && guardedAttributes.has(prop.name)
          && prop.value?.content.trim() !== ''
        ) {
          violations.push(
            `static ${prop.name} ${JSON.stringify(prop.value.content)}`,
          );
        }
        if (
          prop.type === 7
          && prop.name === 'bind'
          && prop.arg !== undefined
          && guardedAttributes.has(prop.arg.content)
          && prop.exp !== undefined
        ) {
          for (const text of expressionLiterals(prop.exp.content)) {
            if (!allowed(path, text)) {
              violations.push(
                `bound ${prop.arg.content} ${JSON.stringify(text)}`,
              );
            }
          }
        }
      }
      for (const child of node.children ?? []) walk(child);
    } else if (node.type === 0) {
      for (const child of node.children ?? []) walk(child);
    } else if (node.type === 9) {
      for (const branch of node.branches ?? []) walk(branch);
    } else if (node.type === 5) {
      const content = (node as unknown as { content?: { content?: string } })
        .content?.content;
      if (content !== undefined) {
        for (const text of expressionLiterals(content)) {
          if (!allowed(path, text)) {
            violations.push(`interpolation ${JSON.stringify(text)}`);
          }
        }
      }
    } else if (node.type === 10 || node.type === 11) {
      for (const child of node.children ?? []) walk(child);
    }
  };
  walk(parsed as unknown as TemplateNode);
  return violations.map((violation) => `${path}: ${violation}`);
}

function scriptViolations(path: string, input: string): string[] {
  const file = ts.createSourceFile(path, input, ts.ScriptTarget.Latest, true);
  const violations: string[] = [];
  const report = (node: ts.Node, text: string): void => {
    if (!allowed(path, text)) {
      const line = file.getLineAndCharacterOfPosition(node.getStart()).line + 1;
      violations.push(`${path}:${line}: ${JSON.stringify(text)}`);
    }
  };
  const isDisplayFunction = (node: ts.Node): boolean => {
    if (!ts.isFunctionDeclaration(node) || node.name === undefined) {
      return false;
    }
    return displayKeys.has(node.name.text)
      || /(?:label|message|text)$/iu.test(node.name.text);
  };
  const hasDisplayContext = (node: ts.Node): boolean => {
    let current: ts.Node = node;
    while (current.parent !== undefined && !ts.isSourceFile(current.parent)) {
      const parent = current.parent;
      if (ts.isPropertyAssignment(parent)) {
        const name = propertyName(parent.name);
        return current === parent.initializer
          && name !== undefined
          && displayKeys.has(name);
      }
      if (ts.isVariableDeclaration(parent)) {
        const name = propertyName(parent.name);
        return current === parent.initializer
          && name !== undefined
          && displayKeys.has(name);
      }
      if (ts.isReturnStatement(parent)) {
        let owner: ts.Node | undefined = parent.parent;
        while (owner !== undefined && !ts.isFunctionDeclaration(owner)) {
          owner = owner.parent;
        }
        return owner !== undefined && isDisplayFunction(owner);
      }
      if (ts.isConditionalExpression(parent) && current === parent.condition) {
        return false;
      }
      if (ts.isCallExpression(parent) || ts.isArrayLiteralExpression(parent)) {
        return false;
      }
      current = parent;
    }
    return false;
  };
  const walk = (node: ts.Node): void => {
    if (ts.isCallExpression(node) && promptCall(node)) {
      const [message] = node.arguments;
      if (message !== undefined) {
        for (const text of literalTexts(message)) report(message, text);
      }
    }
    for (const text of literalTexts(node)) {
      if (hasDisplayContext(node)) report(node, text);
    }
    ts.forEachChild(node, walk);
  };
  walk(file);
  return violations;
}

function promptCall(node: ts.CallExpression): boolean {
  const expression = node.expression;
  if (ts.isIdentifier(expression)) {
    return ['alert', 'confirm', 'prompt'].includes(expression.text);
  }
  if (!ts.isPropertyAccessExpression(expression)) return false;
  const receiver = expression.expression;
  return ts.isIdentifier(receiver)
    && ['globalThis', 'window'].includes(receiver.text)
    && ['alert', 'confirm', 'prompt'].includes(expression.name.text);
}

function sourceViolations(path: string, input = source(path)): string[] {
  if (path.endsWith('.vue')) {
    const descriptor = parseSfc(input, { filename: path }).descriptor;
    return [
      ...(descriptor.template === null || descriptor.template === undefined
        ? []
        : templateViolations(path, descriptor.template.content)),
      ...descriptor.scriptSetup === null || descriptor.scriptSetup === undefined
        ? []
        : scriptViolations(path, descriptor.scriptSetup.content),
      ...descriptor.script === null || descriptor.script === undefined
        ? []
        : scriptViolations(path, descriptor.script.content),
    ];
  }
  return scriptViolations(path, input);
}

function plainObject(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}

function parityViolations(
  vietnamese: unknown,
  english: unknown,
  path = 'copy',
): string[] {
  if (typeof vietnamese === 'function' || typeof english === 'function') {
    if (typeof vietnamese !== typeof english) {
      return [`${path}: function shape`];
    }
    const fixture = functionFixtures[path];
    if (fixture === undefined) return [`${path}: missing function fixture`];
    const vi = (vietnamese as (...args: unknown[]) => unknown)(...fixture);
    const en = (english as (...args: unknown[]) => unknown)(...fixture);
    return typeof vi === 'string' && vi.trim() !== ''
      && typeof en === 'string' && en.trim() !== ''
      ? []
      : [`${path}: empty function value`];
  }
  if (Array.isArray(vietnamese) || Array.isArray(english)) {
    if (!Array.isArray(vietnamese) || !Array.isArray(english)) {
      return [`${path}: array shape`];
    }
    if (vietnamese.length !== english.length) {
      return [`${path}: array length`];
    }
    return vietnamese.flatMap((value, index) =>
      parityViolations(value, english[index], `${path}[${index}]`),
    );
  }
  if (!plainObject(vietnamese) || !plainObject(english)) {
    if (typeof vietnamese !== typeof english) return [`${path}: value shape`];
    return typeof vietnamese === 'string' && vietnamese.trim() === ''
      ? [`${path}: empty Vietnamese value`]
      : typeof english === 'string' && english.trim() === ''
        ? [`${path}: empty English value`]
        : [];
  }
  const viKeys = Object.keys(vietnamese).sort();
  const enKeys = Object.keys(english).sort();
  if (viKeys.join('\0') !== enKeys.join('\0')) {
    return [`${path}: keys ${viKeys.join(',')} !== ${enKeys.join(',')}`];
  }
  return viKeys.flatMap((key) =>
    parityViolations(vietnamese[key], english[key], `${path}.${key}`),
  );
}

function filesBelow(directory: string): string[] {
  if (!existsSync(directory)) return [];
  if (!statSync(directory).isDirectory()) return [directory];
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const file = join(directory, entry.name);
    return entry.isDirectory() ? filesBelow(file) : [file];
  });
}

function scriptContents(path: string, input: string): string {
  if (!path.endsWith('.vue')) return input;
  const descriptor = parseSfc(input, { filename: path }).descriptor;
  return [descriptor.script?.content, descriptor.scriptSetup?.content]
    .filter((content): content is string => content !== undefined)
    .join('\n');
}

function resolvedImport(file: string, specifier: string): string {
  const target = specifier.startsWith('@/') || specifier.startsWith('~/')
    ? join(appRoot, specifier.slice(2))
    : resolve(join(appRoot, file, '..'), specifier);
  return relative(appRoot, target).replace(/\\/gu, '/').replace(/\.ts$/u, '');
}

function isWorkspaceCatalog(file: string, specifier: string): boolean {
  return workspaceCatalogNames.has(
    resolvedImport(file, specifier).replace(/^i18n\//u, ''),
  );
}

function typeOnlyWorkspaceCopy(node: ts.ImportDeclaration): boolean {
  const clause = node.importClause;
  if (clause === undefined || !clause.isTypeOnly) return false;
  const named = clause.namedBindings;
  return named !== undefined
    && ts.isNamedImports(named)
    && named.elements.length > 0
    && named.elements.every((element) => element.name.text === 'WorkspaceCopy');
}

function workspaceImportViolations(file: string, input: string): string[] {
  const script = ts.createSourceFile(
    file,
    scriptContents(file, input),
    ts.ScriptTarget.Latest,
    true,
  );
  const violations: string[] = [];
  const walk = (node: ts.Node): void => {
    if (
      ts.isImportDeclaration(node)
      && ts.isStringLiteral(node.moduleSpecifier)
      && isWorkspaceCatalog(file, node.moduleSpecifier.text)
      && !typeOnlyWorkspaceCopy(node)
    ) {
      violations.push(`${file} imports ${node.moduleSpecifier.text}`);
    }
    if (
      ts.isCallExpression(node)
      && node.expression.kind === ts.SyntaxKind.ImportKeyword
      && node.arguments.length === 1
      && ts.isStringLiteral(node.arguments[0])
      && isWorkspaceCatalog(file, node.arguments[0].text)
    ) {
      violations.push(`${file} imports ${node.arguments[0].text}`);
    }
    ts.forEachChild(node, walk);
  };
  walk(script);
  return violations;
}

describe('workspace localization source guard', () => {
  test.each([
    ['static text', '<template><p>Current icon</p></template>'],
    [
      'aria-label',
      '<template><button aria-label="Current icon" /></template>',
    ],
    ['label', '<template><input label="Current icon" /></template>'],
    ['title', '<template><button title="Current icon" /></template>'],
    [
      'placeholder',
      '<template><input placeholder="Current icon" /></template>',
    ],
    [
      'description',
      '<template><Card description="Current icon" /></template>',
    ],
    [
      'confirm-label',
      '<template><Dialog confirm-label="Current icon" /></template>',
    ],
    [
      'cancel-label',
      '<template><Dialog cancel-label="Current icon" /></template>',
    ],
    [
      'close-label',
      '<template><Dialog close-label="Current icon" /></template>',
    ],
    [
      'bound label',
      '<template><Button :label="\'Current icon\'" /></template>',
    ],
    ['interpolation', '<template>{{ \'Current icon\' }}</template>'],
  ])('rejects %s in a template', (_kind, fixture) => {
    expect(templateViolations('fixture.vue', fixture)).not.toEqual([]);
  });

  test('rejects display literals in scripts and helpers', () => {
    expect(scriptViolations(
      'fixture.ts',
      'const icon = { label: \'Current icon\' };',
    ))
      .not.toEqual([]);
    expect(scriptViolations(
      'fixture.ts',
      'function displayMessage() { return \'Current icon\'; }',
    ))
      .not.toEqual([]);
    expect(scriptViolations(
      'fixture.ts',
      'export const message = \'Current icon\';',
    ))
      .not.toEqual([]);
    expect(scriptViolations(
      'fixture.ts',
      'function displayMessage() { return `Current ${"icon"}`; }',
    )).not.toEqual([]);
    expect(scriptViolations(
      'fixture.ts',
      'window.prompt(\'Current icon\');',
    )).not.toEqual([]);
    expect(scriptViolations(
      'fixture.ts',
      'globalThis.confirm(\'Current icon\');',
    )).not.toEqual([]);
    expect(scriptViolations('fixture.ts', 'alert(\'Current icon\');'))
      .not.toEqual([]);
  });

  test('has no unapproved workspace copy literals', () => {
    const violations = [...vueSources, ...helperSources]
      .flatMap((path) => sourceViolations(path));
    expect(violations).toEqual([]);
  });

  test('keeps each workspace catalog typed, complete, and nonempty', () => {
    for (const path of catalogs) {
      expect(source(path)).toMatch(/(?:WorkspaceCopy|Record<Locale)/u);
    }
    const copies = [
      ['editor-controls', editorControlsCopy],
      ['editor-fields', editorFieldsCopy],
      ['editor-sections', editorSectionsCopy],
      ['editor-shell', editorShellCopy],
      ['meta', workspaceTitles],
      ['pdf', pdfCopy],
      ['publish', publishCopy],
      ['resume-create', resumeCreateCopy],
      ['resume-list', resumeListCopy],
      ['shell', shellCopy],
      ['workspace', workspaceCopy],
    ];
    for (const [name, copy] of copies) {
      expect(Object.keys(copy).sort()).toEqual(['en', 'vi']);
      expect(parityViolations(copy.vi, copy.en, name)).toEqual([]);
    }
  });

  test('rejects catalog arrays with different shapes', () => {
    expect(parityViolations(['one'], ['one', 'two'])).not.toEqual([]);
  });

  test('does not keep English message constants in semantic helpers', () => {
    const forbidden = [
      'You have reached the resume limit',
      'Could not create your resume',
      'Please wait and try again',
      'Your session has ended',
      'Add an end date or tick Present',
      'PDF download failed',
    ];
    const paths = [
      'composables/useResumeList.ts',
      'pages/app/new.vue',
      'components/editor/forms/ResumeLanguageField.vue',
      'editor/pdfDownload.ts',
    ];
    for (const path of paths) {
      const input = source(path);
      for (const text of forbidden) expect(input).not.toContain(text);
    }
  });

  test('keeps workspace catalogs out of pure output roots', () => {
    const roots = [
      'components/print',
      'components/public',
      'components/resume',
      'public',
      '../server/plugins/public-render-worker.ts',
      '../server/routes/internal-render',
      '../server/routes/print',
      '../server/utils/print',
      '../server/utils/public-render',
      '../server/workers/print',
      '../server/workers/public-render',
    ];
    const sourceFiles = roots.flatMap((root) =>
      filesBelow(join(appRoot, root)),
    );
    expect(sourceFiles.length).toBeGreaterThan(0);
    const violations = sourceFiles.flatMap((file) => {
      if (!/\.(?:ts|vue)$/u.test(file)) return [];
      return workspaceImportViolations(
        relative(appRoot, file),
        readFileSync(file, 'utf8'),
      );
    });
    expect(violations).toEqual([]);
  });

  test('rejects static workspace imports under pure roots', () => {
    expect(workspaceImportViolations(
      'components/resume/fixture.ts',
      'import { workspaceCopy } from \'@/i18n/workspace\';',
    )).not.toEqual([]);
    expect(workspaceImportViolations(
      'components/resume/fixture.ts',
      'void import(\'../../i18n/workspace\');',
    )).not.toEqual([]);
    expect(workspaceImportViolations(
      'components/resume/fixture.ts',
      'import type { WorkspaceCopy } from \'../../i18n/workspace\';',
    )).toEqual([]);
  });
});
