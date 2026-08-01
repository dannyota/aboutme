// @ts-check
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

export default withNuxt({
  rules: googleStyleRules,
});
