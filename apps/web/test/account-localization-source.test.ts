import { existsSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import ts from 'typescript';
import { describe, expect, test } from 'vitest';

import { agentSettingsCopy } from '../app/i18n/agent-settings';
import { consentCopy } from '../app/i18n/consent';
import { identitySettingsCopy } from '../app/i18n/identity-settings';
import { passwordSettingsCopy } from '../app/i18n/password-settings';
import { privacySettingsCopy } from '../app/i18n/privacy-settings';
import { settingsCopy } from '../app/i18n/settings';
import {
  appRoot,
  filesBelow,
  literalGuard,
  parityViolations,
  resolvedImport,
  scriptContents,
} from './support/localizationSource';

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
  'components/settings/ReauthPrompt.vue',
  'pages/app/settings/sessions.vue',
  'pages/authorize.vue',
];
const approvedLiterals: Readonly<Record<string, readonly string[]>> = {
  'components/settings/PrivacySettings.vue': ['DELETE'],
};
const { sourceViolations } = literalGuard(approvedLiterals);

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
