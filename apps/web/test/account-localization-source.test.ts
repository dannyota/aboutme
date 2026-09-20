import { parse as parseTemplate } from '@vue/compiler-dom';
import { parse as parseSfc } from '@vue/compiler-sfc';
import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative, resolve } from 'node:path';
import ts from 'typescript';
import { describe, expect, test } from 'vitest';

import { agentSettingsCopy } from '../app/i18n/agent-settings';
import { consentCopy } from '../app/i18n/consent';
import { identitySettingsCopy } from '../app/i18n/identity-settings';
import { passwordSettingsCopy } from '../app/i18n/password-settings';
import { privacySettingsCopy } from '../app/i18n/privacy-settings';
import { settingsCopy } from '../app/i18n/settings';

const appRoot = resolve(process.cwd(), 'app');
const catalogNames = new Set([
  'agent-settings',
  'consent',
  'identity-settings',
  'password-settings',
  'privacy-settings',
  'settings',
]);
const coveredSources = [
  'components/auth/PasswordSettings.vue',
  'components/settings/ConnectedAgents.vue',
  'components/settings/LinkedIdentities.vue',
  'components/settings/PrivacySettings.vue',
  'pages/app/settings/sessions.vue',
  'pages/authorize.vue',
];
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
const approvedLiterals: Readonly<Record<string, readonly string[]>> = {
  'components/settings/PrivacySettings.vue': ['DELETE'],
};

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

function allowed(path: string, text: string): boolean {
  return approvedLiterals[path]?.includes(text) ?? false;
}

function literalTexts(node: ts.Node): readonly string[] {
  if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) {
    return node.text === '' ? [] : [node.text];
  }
  if (ts.isTemplateExpression(node)) {
    return [
      node.head.text,
      ...node.templateSpans.map((span) => span.literal.text),
    ].filter((text) => text.trim() !== '');
  }
  return [];
}

function expressionLiterals(input: string): readonly string[] {
  const file = ts.createSourceFile(
    'template.ts',
    input,
    ts.ScriptTarget.Latest,
  );
  const statement = file.statements[0];
  if (statement === undefined || !ts.isExpressionStatement(statement)) {
    return [];
  }
  const literals: string[] = [];
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
  walk(statement.expression);
  return literals;
}

function templateViolations(path: string, input: string): string[] {
  const parsed = parseTemplate(input, { comments: false });
  const violations: string[] = [];
  const report = (kind: string, text: string): void => {
    if (!allowed(path, text)) {
      violations.push(`${path}: ${kind} ${JSON.stringify(text)}`);
    }
  };
  const walk = (node: TemplateNode): void => {
    if (node.type === 2 && node.content?.trim() !== '') {
      report('static text', node.content.trim());
    }
    if (node.type === 1) {
      for (const prop of node.props ?? []) {
        if (
          prop.type === 6
          && guardedAttributes.has(prop.name)
          && prop.value?.content.trim() !== ''
        ) {
          report(`static ${prop.name}`, prop.value.content);
        }
        if (
          prop.type === 7
          && prop.name === 'bind'
          && prop.arg !== undefined
          && guardedAttributes.has(prop.arg.content)
          && prop.exp !== undefined
        ) {
          for (const text of expressionLiterals(prop.exp.content)) {
            report(`bound ${prop.arg.content}`, text);
          }
        }
      }
      for (const child of node.children ?? []) walk(child);
    } else if (node.type === 0 || node.type === 10 || node.type === 11) {
      for (const child of node.children ?? []) walk(child);
    } else if (node.type === 9) {
      for (const branch of node.branches ?? []) walk(branch);
    } else if (node.type === 5) {
      const content = (node as unknown as { content?: { content?: string } })
        .content?.content;
      if (content !== undefined) {
        for (const text of expressionLiterals(content)) {
          report('interpolation', text);
        }
      }
    }
  };
  walk(parsed as unknown as TemplateNode);
  return violations;
}

function propertyName(
  name: ts.PropertyName | ts.BindingName,
): string | undefined {
  return ts.isIdentifier(name) || ts.isStringLiteral(name)
    ? name.text
    : undefined;
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
  const displayFunction = (node: ts.Node): boolean => {
    if (!ts.isFunctionDeclaration(node) || node.name === undefined) {
      return false;
    }
    return (
      displayKeys.has(node.name.text)
      || /(?:label|message|text)$/iu.test(node.name.text)
    );
  };
  const displayContext = (node: ts.Node): boolean => {
    let current: ts.Node = node;
    while (current.parent !== undefined && !ts.isSourceFile(current.parent)) {
      const parent = current.parent;
      if (ts.isPropertyAssignment(parent) || ts.isVariableDeclaration(parent)) {
        const name = propertyName(parent.name);
        return (
          current === parent.initializer
          && name !== undefined
          && displayKeys.has(name)
        );
      }
      if (ts.isReturnStatement(parent)) {
        let owner: ts.Node | undefined = parent.parent;
        while (owner !== undefined && !ts.isFunctionDeclaration(owner)) {
          owner = owner.parent;
        }
        return owner !== undefined && displayFunction(owner);
      }
      if (ts.isCallExpression(parent) || ts.isArrayLiteralExpression(parent)) {
        return false;
      }
      current = parent;
    }
    return false;
  };
  const walk = (node: ts.Node): void => {
    for (const text of literalTexts(node)) {
      if (displayContext(node)) report(node, text);
    }
    ts.forEachChild(node, walk);
  };
  walk(file);
  return violations;
}

function sourceViolations(path: string): string[] {
  const input = source(path);
  if (!path.endsWith('.vue')) return scriptViolations(path, input);
  const descriptor = parseSfc(input, { filename: path }).descriptor;
  return [
    ...(descriptor.template === null
      ? []
      : templateViolations(path, descriptor.template.content)),
    ...(descriptor.script === null
      ? []
      : scriptViolations(path, descriptor.script.content)),
    ...(descriptor.scriptSetup === null
      ? []
      : scriptViolations(path, descriptor.scriptSetup.content)),
  ];
}

function isObject(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}

function parityViolations(vi: unknown, en: unknown, path = 'copy'): string[] {
  if (typeof vi === 'function' || typeof en === 'function') {
    return typeof vi === typeof en
      && (vi as { readonly length: number }).length
      === (en as { readonly length: number }).length
      ? []
      : [`${path}: function shape`];
  }
  if (!isObject(vi) || !isObject(en)) {
    return typeof vi === typeof en
      && typeof vi === 'string'
      && vi.trim() !== ''
      && en.trim() !== ''
      ? []
      : [`${path}: value shape`];
  }
  const viKeys = Object.keys(vi).sort();
  const enKeys = Object.keys(en).sort();
  if (viKeys.join('\0') !== enKeys.join('\0')) {
    return [`${path}: keys ${viKeys.join(',')} !== ${enKeys.join(',')}`];
  }
  return viKeys.flatMap((key) =>
    parityViolations(vi[key], en[key], `${path}.${key}`),
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
  const target
    = specifier.startsWith('@/') || specifier.startsWith('~/')
      ? join(appRoot, specifier.slice(2))
      : resolve(join(appRoot, file, '..'), specifier);
  return relative(appRoot, target).replace(/\\/gu, '/').replace(/\.ts$/u, '');
}

type FixtureSources = Readonly<Record<string, string>>;

function moduleCandidates(file: string, specifier: string): string[] {
  const base = resolvedImport(file, specifier);
  if (
    (base.startsWith('../') && !base.startsWith('../server/'))
    || base === '..'
  ) return [];
  return [
    base,
    `${base}.ts`,
    `${base}.vue`,
    `${base}/index.ts`,
    `${base}/index.vue`,
  ];
}

function accountCatalogImportViolations(
  file: string,
  input: string,
  fixtures: FixtureSources = {},
  visited = new Set<string>(),
): string[] {
  const violations: string[] = [];
  const readModule = (path: string): string | undefined => {
    if (path in fixtures) return fixtures[path];
    const absolute = join(appRoot, path);
    return existsSync(absolute) && statSync(absolute).isFile()
      ? readFileSync(absolute, 'utf8')
      : undefined;
  };
  const visit = (
    path: string,
    contents: string,
    chain: readonly string[],
  ): void => {
    if (visited.has(path)) return;
    visited.add(path);
    const script = ts.createSourceFile(
      path,
      scriptContents(path, contents),
      ts.ScriptTarget.Latest,
      true,
    );
    const follow = (specifier: string): void => {
      const catalog = catalogNames.has(
        resolvedImport(path, specifier).replace(/^i18n\//u, ''),
      );
      if (catalog) {
        violations.push(`${chain.join(' -> ')} imports ${specifier}`);
        return;
      }
      for (const candidate of moduleCandidates(path, specifier)) {
        const imported = readModule(candidate);
        if (imported !== undefined) {
          visit(candidate, imported, [...chain, candidate]);
          return;
        }
      }
    };
    const walk = (node: ts.Node): void => {
      if (
        (ts.isImportDeclaration(node) || ts.isExportDeclaration(node))
        && node.moduleSpecifier !== undefined
        && ts.isStringLiteral(node.moduleSpecifier)
      ) {
        follow(node.moduleSpecifier.text);
      }
      if (
        ts.isCallExpression(node)
        && node.expression.kind === ts.SyntaxKind.ImportKeyword
        && node.arguments.length === 1
        && ts.isStringLiteral(node.arguments[0])
      ) {
        follow(node.arguments[0].text);
      }
      ts.forEachChild(node, walk);
    };
    walk(script);
  };
  visit(file, input, [file]);
  return violations;
}

function accountCatalogViolationsFromRoots(files: readonly string[]): string[] {
  const visited = new Set<string>();
  return files.flatMap((file) =>
    accountCatalogImportViolations(
      relative(appRoot, file),
      readFileSync(file, 'utf8'),
      {},
      visited,
    ),
  );
}

describe('account localization source guard', () => {
  test('keeps the six account catalogs typed, complete, and aligned', () => {
    const catalogs = [
      ['agent-settings', agentSettingsCopy],
      ['consent', consentCopy],
      ['identity-settings', identitySettingsCopy],
      ['password-settings', passwordSettingsCopy],
      ['privacy-settings', privacySettingsCopy],
      ['settings', settingsCopy],
    ] as const;
    for (const [name, copy] of catalogs) {
      expect(Object.keys(copy).sort()).toEqual(['en', 'vi']);
      expect(parityViolations(copy.vi, copy.en, name)).toEqual([]);
    }
  });

  test('has no uncovered account or consent display literals', () => {
    expect(coveredSources.flatMap((path) => sourceViolations(path))).toEqual(
      [],
    );
  });

  test(
    'keeps account catalogs out of output and unrelated route roots',
    () => {
      const roots = [
        'app.vue',
        'components/print',
        'components/public',
        'components/resume',
        'components/editor',
        'editor',
        'pages/index.vue',
        'pages/templates',
        'pages/login.vue',
        'pages/register.vue',
        'pages/forgot-password.vue',
        'pages/reset-password.vue',
        'pages/verify-email.vue',
        'pages/app/resumes',
        '../server/plugins/public-render-worker.ts',
        '../server/routes/internal-render',
        '../server/routes/print',
        '../server/utils/print',
        '../server/utils/public-render',
        '../server/workers/print',
        '../server/workers/public-render',
      ];
      const sourceFiles = roots
        .flatMap((root) => filesBelow(join(appRoot, root)))
        .filter((file) => /\.(?:ts|vue)$/u.test(file));
      expect(sourceFiles.length).toBeGreaterThan(0);
      const violations = accountCatalogViolationsFromRoots(sourceFiles);
      expect(violations).toEqual([]);
    },
  );

  test('rejects a catalog import under an unrelated route root', () => {
    expect(
      accountCatalogImportViolations(
        'pages/templates/fixture.ts',
        'import { consentCopy } from \'@/i18n/consent\';',
      ),
    ).not.toEqual([]);
  });

  test('rejects a catalog reachable through a shared helper', () => {
    const fixtures = {
      'shared/consent-helper.ts':
        'export { consentCopy } from \'@/i18n/consent\';',
    };
    expect(
      accountCatalogImportViolations(
        'pages/templates/fixture.ts',
        'import { consentCopy } from \'../../shared/consent-helper\';',
        fixtures,
      ),
    ).not.toEqual([]);
  });

  test('rejects a catalog reachable from the global shell', () => {
    const fixtures = {
      'components/app/AppShell.vue':
        [
          '<script setup>',
          'import { consentCopy } from \'@/i18n/consent\';',
          '</script>',
        ].join('\n'),
    };
    expect(
      accountCatalogImportViolations(
        'app.vue',
        [
          '<script setup>',
          'import AppShell from \'./components/app/AppShell.vue\';',
          '</script>',
        ].join('\n'),
        fixtures,
      ),
    ).not.toEqual([]);
  });

  test('rejects a catalog reachable through a dynamic import', () => {
    const fixtures = {
      'shared/consent-helper.ts':
        'export { consentCopy } from \'@/i18n/consent\';',
    };
    expect(
      accountCatalogImportViolations(
        'pages/templates/fixture.ts',
        'void import(\'../../shared/consent-helper\');',
        fixtures,
      ),
    ).not.toEqual([]);
  });

  test('checks every root while visiting shared imports once', () => {
    const fixtures = {
      'shared/consent-helper.ts':
        'export { consentCopy } from \'@/i18n/consent\';',
    };
    const visited = new Set<string>();
    const violations = [
      ...accountCatalogImportViolations(
        'pages/templates/first.ts',
        'import \'../../shared/consent-helper\';',
        fixtures,
        visited,
      ),
      ...accountCatalogImportViolations(
        'pages/templates/second.ts',
        'import \'../../shared/consent-helper\';',
        fixtures,
        visited,
      ),
    ];

    expect(violations).toHaveLength(1);
    expect(visited).toEqual(
      new Set([
        'pages/templates/first.ts',
        'shared/consent-helper.ts',
        'pages/templates/second.ts',
      ]),
    );
  });
});
