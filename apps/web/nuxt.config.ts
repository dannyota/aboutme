// https://nuxt.com/docs/api/configuration/nuxt-config
export default defineNuxtConfig({
  modules: ['@nuxt/eslint'],

  // Server-side rendering is required: public resume pages and SEO/GEO
  // surfaces must be crawlable without JS (spec §5).
  ssr: true,

  devtools: { enabled: true },

  css: ['~/assets/css/fonts.css'],

  runtimeConfig: {
    public: {
      // Go API base path. Same-origin in dev/prod (Caddy routes /api/v1/*
      // to the server); override only for local experimentation.
      apiBase: '/api/v1',
    },
  },

  devServer: {
    port: 3000,
  },
  compatibilityDate: '2026-08-01',

  eslint: {
    config: {
      stylistic: {
        indent: 2,
        quotes: 'single',
        semi: true,
        braceStyle: '1tbs',
        arrowParens: true,
        commaDangle: 'always-multiline',
      },
    },
  },
});
