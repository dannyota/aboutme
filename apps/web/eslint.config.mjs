// @ts-check
import { dirname, resolve, sep } from 'node:path';
import withNuxt from './.nuxt/eslint.config.mjs';

// Google TypeScript Style (https://google.github.io/styleguide/tsguide.html)
// essentials not already covered by the generated Nuxt/Vue/TS flat config's
// stylistic block (2-space indent, single quotes, semicolons, 1tbs braces,
// multiline trailing commas — configured in nuxt.config.ts `eslint.config`).
const googleStyleRules = {
  'curly': ['error', 'multi-line'],
  'eqeqeq': ['error', 'always'],
  'no-var': 'error',
  'prefer-const': ['error', { destructuring: 'all' }],
  'camelcase': ['error', { properties: 'never' }],
  'max-len': [
    'error',
    { code: 80, tabWidth: 2, ignoreUrls: true, ignoreComments: false },
  ],
};

const forbiddenRendererCalls = new Map([
  ['Date', new Set(['now'])],
  ['Math', new Set(['random'])],
  ['crypto', new Set(['getRandomValues', 'randomUUID'])],
  ['performance', new Set(['now'])],
]);

const globalObjects = new Set(['globalThis', 'window', 'self']);
const ambientRuntimeGlobals = new Set([
  ...globalObjects,
  'navigator',
  'process',
]);
const forbiddenNetworkGlobals = new Set([
  '$fetch',
  'EventSource',
  'fetch',
  'WebSocket',
  'XMLHttpRequest',
]);
const allowedRendererImports = [
  /^@aboutme\/schema(?:\/.*)?$/,
  /^@lucide\/vue$/,
  /^vue$/,
];
const rendererRoot = resolve(process.cwd(), 'app/components/resume');
const allowedRendererUtilities = new Set([
  resolve(process.cwd(), 'app/utils/fontCatalog'),
  resolve(process.cwd(), 'app/utils/fontsReady'),
  resolve(process.cwd(), 'app/utils/sanitizeRichText'),
]);

function staticPropertyName(node) {
  if (!node.computed && node.property.type === 'Identifier') {
    return node.property.name;
  }
  if (
    node.computed
    && node.property.type === 'Literal'
    && typeof node.property.value === 'string'
  ) {
    return node.property.value;
  }
  return null;
}

function rootIdentifier(node) {
  let current = node;
  while (current.type === 'MemberExpression') {
    current = current.object;
  }
  return current.type === 'Identifier' ? current : null;
}

function globalMember(node) {
  if (node.type !== 'MemberExpression') return null;
  const object = node.object;
  if (object.type !== 'Identifier' || !globalObjects.has(object.name)) {
    return null;
  }
  const property = staticPropertyName(node);
  return property === null ? null : { object, property };
}

function unwrappedGlobalRoot(sourceCode, node) {
  const current = node;
  if (current.type === 'MemberExpression') {
    const member = globalMember(current);
    if (member !== null && isGlobalIdentifier(sourceCode, member.object)) {
      return { name: member.property, node: current };
    }
  }
  const root = rootIdentifier(current);
  if (root === null || !isGlobalIdentifier(sourceCode, root)) return null;
  return { name: root.name, node: root };
}

const noRendererImportBoundary = {
  meta: {
    type: 'problem',
    docs: {
      description: 'Keep renderer imports inside its pure dependency set.',
    },
    schema: [],
    messages: {
      forbidden: 'Resume renderers cannot import {{dependency}}.',
    },
  },
  create(context) {
    const check = (node) => {
      const dependency = node.source?.value;
      if (dependency === undefined && node.type !== 'ImportExpression') {
        return;
      }
      const resolvedDependency
        = typeof dependency === 'string' && /^\.\.?\//.test(dependency)
          ? resolve(dirname(context.filename), dependency)
          : null;
      if (
        typeof dependency === 'string'
        && (allowedRendererImports.some((pattern) => pattern.test(dependency))
          || (resolvedDependency !== null
            && (resolvedDependency.startsWith(`${rendererRoot}${sep}`)
              || allowedRendererUtilities.has(resolvedDependency))))
      ) {
        return;
      }
      context.report({
        node: node.source ?? node,
        messageId: 'forbidden',
        data: {
          dependency:
            typeof dependency === 'string' ? dependency : '<dynamic>',
        },
      });
    };
    return {
      ExportAllDeclaration: check,
      ExportNamedDeclaration: check,
      ImportExpression: check,
      ImportDeclaration: check,
    };
  },
};

function isGlobalIdentifier(sourceCode, identifier) {
  let scope = sourceCode.getScope(identifier);
  while (scope !== null) {
    const variable = scope.set.get(identifier.name);
    if (variable !== undefined) {
      return variable.defs.length === 0;
    }
    scope = scope.upper;
  }
  return true;
}

const noRendererNondeterminism = {
  meta: {
    type: 'problem',
    docs: {
      description: 'Disallow nondeterministic operations in resume renderers.',
    },
    schema: [],
    messages: {
      forbidden: 'Resume renderers cannot use {{dependency}}.',
    },
  },
  create(context) {
    const { sourceCode } = context;
    const report = (node, dependency) =>
      context.report({
        node,
        messageId: 'forbidden',
        data: { dependency },
      });

    return {
      CallExpression(node) {
        if (node.callee.type === 'Identifier') {
          if (
            (node.callee.name === 'Date'
              || forbiddenNetworkGlobals.has(node.callee.name))
            && isGlobalIdentifier(sourceCode, node.callee)
          ) {
            report(node, `${node.callee.name}()`);
          }
          return;
        }
        if (node.callee.type !== 'MemberExpression') {
          return;
        }
        const property = staticPropertyName(node.callee);
        if (property?.startsWith('toLocale')) {
          report(node, `${property}()`);
          return;
        }

        const member = globalMember(node.callee);
        if (
          member !== null
          && isGlobalIdentifier(sourceCode, member.object)
          && forbiddenNetworkGlobals.has(member.property)
        ) {
          report(node, `${member.object.name}.${member.property}()`);
          return;
        }

        const root = unwrappedGlobalRoot(sourceCode, node.callee.object);
        if (root === null) {
          return;
        }
        if (forbiddenRendererCalls.get(root.name)?.has(property)) {
          report(node, `${root.name}.${property}()`);
        }
      },
      NewExpression(node) {
        if (
          node.callee.type === 'Identifier'
          && (node.callee.name === 'Date'
            || forbiddenNetworkGlobals.has(node.callee.name))
          && isGlobalIdentifier(sourceCode, node.callee)
        ) {
          report(node, `new ${node.callee.name}()`);
          return;
        }
        const member = globalMember(node.callee);
        if (
          member !== null
          && isGlobalIdentifier(sourceCode, member.object)
          && (member.property === 'Date'
            || forbiddenNetworkGlobals.has(member.property))
        ) {
          report(node, `new ${member.object.name}.${member.property}()`);
        }
      },
      Identifier(node) {
        if (
          ambientRuntimeGlobals.has(node.name)
          && isGlobalIdentifier(sourceCode, node)
        ) {
          report(node, `the ambient ${node.name} namespace`);
          return;
        }
        if (
          !(
            node.name === 'Intl'
            || globalObjects.has(node.name)
            || forbiddenRendererCalls.has(node.name)
          )
          || !isGlobalIdentifier(sourceCode, node)
          || (node.parent.type === 'MemberExpression'
            && node.parent.object === node)
          || (node.parent.type === 'Property'
            && node.parent.key === node
            && !node.parent.computed
            && !node.parent.shorthand)
        ) {
          return;
        }
        report(
          node,
          node.name === 'Intl'
            ? 'the Intl namespace'
            : `the ${node.name} namespace`,
        );
      },
      MemberExpression(node) {
        const property = staticPropertyName(node);
        const wrapped = globalMember(node);
        if (
          property === null
          && node.object.type === 'Identifier'
          && globalObjects.has(node.object.name)
          && isGlobalIdentifier(sourceCode, node.object)
        ) {
          report(node, `${node.object.name}.<computed>`);
          return;
        }
        if (
          wrapped !== null
          && isGlobalIdentifier(sourceCode, wrapped.object)
          && (wrapped.property === 'Intl'
            || forbiddenRendererCalls.has(wrapped.property)
            || forbiddenNetworkGlobals.has(wrapped.property))
        ) {
          report(node, `${wrapped.object.name}.${wrapped.property}`);
          return;
        }

        const root = unwrappedGlobalRoot(sourceCode, node.object);
        if (
          property === null
          && root !== null
          && (root.name === 'Intl' || forbiddenRendererCalls.has(root.name))
        ) {
          report(node, `${root.name}.<computed>`);
          return;
        }
        if (
          root === null
          || (root.name !== 'Intl'
            && !forbiddenRendererCalls.get(root.name)?.has(property))
        ) {
          return;
        }
        report(
          node,
          root.name === 'Intl'
            ? 'the Intl namespace'
            : `${root.name}.${property}`,
        );
      },
      VariableDeclarator(node) {
        if (node.id.type !== 'ObjectPattern' || node.init === null) return;
        const root = unwrappedGlobalRoot(sourceCode, node.init);
        if (root === null) return;
        for (const propertyNode of node.id.properties) {
          if (propertyNode.type !== 'Property') continue;
          const property
            = propertyNode.key.type === 'Identifier'
              ? propertyNode.key.name
              : propertyNode.key.type === 'Literal'
                && typeof propertyNode.key.value === 'string'
                ? propertyNode.key.value
                : null;
          if (
            property !== null
            && (root.name === 'globalThis'
              || root.name === 'window'
              || root.name === 'self'
              ? property === 'Intl'
              || forbiddenRendererCalls.has(property)
              || forbiddenNetworkGlobals.has(property)
              : root.name === 'Intl'
                || forbiddenRendererCalls.get(root.name)?.has(property))
          ) {
            report(node, `${root.name}.${property}`);
          }
        }
      },
    };
  },
};

const aboutmePlugin = {
  rules: {
    'no-renderer-import-boundary': noRendererImportBoundary,
    'no-renderer-nondeterminism': noRendererNondeterminism,
  },
};

export default withNuxt(
  {
    // Generated by `make api-gen` from docs/api/openapi.yaml — style rules
    // are meaningless on output nobody edits, and `--fix` would rewrite it
    // into permanent drift against the generator. `make api-check`, not
    // eslint, is what keeps this directory honest.
    ignores: ['app/api/generated/**'],
  },
  {
    rules: googleStyleRules,
  },
  {
    files: ['app/components/resume/**/*.{ts,vue,mts}'],
    plugins: {
      aboutme: aboutmePlugin,
    },
    rules: {
      'aboutme/no-renderer-nondeterminism': 'error',
      'aboutme/no-renderer-import-boundary': 'error',
      'no-restricted-globals': [
        'error',
        'fetch',
        '$fetch',
        'XMLHttpRequest',
        'WebSocket',
        'EventSource',
      ],
      'no-restricted-imports': [
        'error',
        {
          paths: ['pinia', '#app'],
          patterns: [
            {
              group: [
                '~/stores/**',
                '~/composables/**',
                '~/components/editor/**',
              ],
            },
          ],
        },
      ],
    },
  },
).append(
  {
    name: 'aboutme/public-source-unignore',
    ignores: [
      '!app/public',
      '!app/public/**',
      '!app/components/public',
      '!app/components/public/**',
    ],
  },
);
