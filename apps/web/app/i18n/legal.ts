// Privacy Policy and Terms of Service in both site languages. Every statement
// here must describe shipped behavior or an owner decision; do not add claims.
import { legalEn } from './legal-en';
import { legalVi } from './legal-vi';
import type { Locale } from './locale';

export const legalContact = 'danny@aboutme.vn';
export const repositoryUrl = 'https://github.com/dannyota/aboutme';

export type LegalSection = {
  readonly heading: string;
  readonly paragraphs?: readonly string[];
  readonly items?: readonly string[];
  /** Text after the list, in the same section. */
  readonly after?: readonly string[];
  /** Ends the section with a link to the source repository. */
  readonly repositoryLink?: string;
  /** Ends the section with this label and the contact address. */
  readonly contactLabel?: string;
};

export type LegalDocument = {
  readonly title: string;
  /** The search result summary; restates the intro, no new claims. */
  readonly description: string;
  readonly intro: string;
  /** Who runs the service, followed by the contact address. */
  readonly operator?: { readonly text: string; readonly contactLabel: string };
  readonly sections: readonly LegalSection[];
};

export type LegalCopy = {
  readonly updated: string;
  readonly privacy: LegalDocument;
  readonly terms: LegalDocument;
  /** Short link labels for the homepage footer and registration. */
  readonly privacyLink: string;
  readonly termsLink: string;
  /** The homepage footer's link to /verify. */
  readonly verifyLink: string;
  /** The homepage footer's link to /guide/mcp, right after Verify. */
  readonly guideLink: string;
  /** "By creating an account you agree to the [Terms] and the [Privacy]." */
  readonly agreement: readonly [string, string, string];
};

export const legalCopy: Record<Locale, LegalCopy> = {
  vi: legalVi,
  en: legalEn,
};
