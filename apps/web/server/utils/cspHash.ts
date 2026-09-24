import { createHash } from 'node:crypto';

// Matches an inline `<script type="application/ld+json">...</script>` tag
// regardless of attribute order, the same lookahead style
// server/utils/cspExternalize.ts uses for the Nuxt payload script. It never
// matches the externalized `id="__NUXT_DATA__"` payload script, which is
// `application/json`, not `application/ld+json`.
const JSON_LD_SCRIPT
  = /<script(?=[^>]*\btype="application\/ld\+json")[^>]*>([\s\S]*?)<\/script>/g;

/**
 * The exact UTF-8 bytes of a rendered page's one inline JSON-LD script, or
 * null when the page has none. The homepage and template pages each render
 * exactly one (app/landing/structuredData.ts,
 * app/templates/structuredData.ts), the same "exactly one deterministic
 * JSON-LD script" invariant public resume HTML already proves
 * (apps/server/internal/publicformat/jsonld.go; docs/design/web.md).
 */
export function jsonLdScriptContent(html: string): string | null {
  const matches = [...html.matchAll(JSON_LD_SCRIPT)];
  if (matches.length === 0) return null;
  if (matches.length > 1) {
    throw new Error(
      `Page has ${matches.length} JSON-LD scripts; expected at most one.`,
    );
  }
  const content = matches[0]?.[1];
  if (content === undefined) {
    throw new Error('JSON-LD script capture failed.');
  }
  return content;
}

/** The CSP `'sha256-<base64>'` source for one script's exact UTF-8 text. */
export function scriptHashSource(content: string): string {
  const digest = createHash('sha256')
    .update(content, 'utf8')
    .digest('base64');
  return `'sha256-${digest}'`;
}

const SCRIPT_SRC_SELF = 'script-src \'self\';';

/**
 * Adds one response-specific script-src source to a base policy that already
 * has a plain `script-src 'self';`, mirroring the response-specific-hash
 * mechanism public resume HTML uses
 * (apps/server/internal/publicformat/jsonld.go).
 */
export function withScriptSource(basePolicy: string, source: string): string {
  if (!basePolicy.includes(SCRIPT_SRC_SELF)) {
    throw new Error(`Base policy is missing "${SCRIPT_SRC_SELF}".`);
  }
  return basePolicy.replace(
    SCRIPT_SRC_SELF,
    `script-src 'self' ${source};`,
  );
}
