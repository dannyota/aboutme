import { readFileSync } from 'node:fs';
import { join, relative } from 'node:path';
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
import {
  appRoot,
  filesBelow,
  literalGuard,
  parityViolations,
  resolvedImport,
  scriptContents,
  source,
} from '../support/localizationSource';

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
const { templateViolations, scriptViolations, sourceViolations }
  = literalGuard(approvedLiterals);

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
      expect(parityViolations(copy.vi, copy.en, name, functionFixtures))
        .toEqual([]);
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
