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
  readonly after?: readonly string[];
  readonly repositoryLink?: string;
  readonly contactLabel?: string;
};

export type LegalDocument = {
  readonly title: string;
  readonly description: string;
  readonly intro: string;
  readonly operator?: { readonly text: string; readonly contactLabel: string };
  readonly sections: readonly LegalSection[];
};

export type LegalCopy = {
  readonly privacyUpdated: string;
  readonly termsUpdated: string;
  readonly privacy: LegalDocument;
  readonly terms: LegalDocument;
  readonly privacyLink: string;
  readonly termsLink: string;
  readonly verifyLink: string;
  readonly guideLink: string;
  readonly agreement: readonly [string, string, string];
};

export const legalCopy: Record<Locale, LegalCopy> = {
  vi: legalVi,
  en: legalEn,
};
