// Mounts the review form with hostile LinkedIn text (docs/design/
// linkedin-import.md, "Tests", the injection-en.pdf fixture) and asserts it
// renders only as text: no script or img element, and every hostile string
// still reads somewhere on the page. pdf.js does not run reliably in the
// Nuxt test environment (happy-dom), so this builds the review from a
// hand-made ParsedProfile carrying the same strings as injection-en.pdf,
// through the real buildReview pipeline, rather than reading the fixture.
import { mountSuspended } from '@nuxt/test-utils/runtime';
import { describe, expect, it } from 'vitest';

import ImportReview from '../../../app/components/import/ImportReview.vue';
import { buildReview } from '../../../app/import/linkedin/build';
import { initialChoiceSets } from '../../../app/import/linkedin/pageState';
import { linesToRichText } from '../../../app/import/linkedin/richText';
import type { ParsedProfile } from '../../../app/import/linkedin/sections';
import { resumeCreateCopy } from '../../../app/i18n/resume-create';
import { importCopy } from '../../../app/i18n/import';

const SCRIPT_TAG = '<script>alert(1)</script>';
const IMG_TAG = '<img src=x onerror=alert(1)>';
const JS_URL = 'javascript:alert(1)';

function makeUuid(): () => string {
  let n = 0;
  return () => {
    n += 1;
    return `00000000-0000-4000-8000-${n.toString(16).padStart(12, '0')}`;
  };
}

/** The same hostile strings as fixtures/injection-en.fo, across every
 * mapped field, without reading the PDF. */
function hostileParsedProfile(): ParsedProfile {
  const summary = linesToRichText([
    { text: SCRIPT_TAG, gap: null },
    { text: IMG_TAG, gap: 6 },
    { text: JS_URL, gap: 6 },
  ]);
  return {
    profile: {
      fullName: `Sample ${SCRIPT_TAG}`,
      headline: `${SCRIPT_TAG} ${JS_URL}`,
      location: IMG_TAG,
    },
    contacts: [
      { type: 'phone', value: '0900000000', label: 'Home' },
      { type: 'email', value: `${SCRIPT_TAG}@example.com` },
      { type: 'website', value: 'https://example.com/script', label: JS_URL },
    ],
    summary,
    work: [{
      employer: SCRIPT_TAG,
      jobTitle: IMG_TAG,
      dates: { start: { y: 2020, m: 1 }, end: null, present: true },
      city: JS_URL,
      country: SCRIPT_TAG,
      description: linesToRichText([{ text: IMG_TAG, gap: null }]).html,
      descriptionCut: false,
    }],
    education: [{
      school: SCRIPT_TAG,
      degree: IMG_TAG,
      dates: { start: { y: 2011 }, end: { y: 2015 }, present: false },
    }],
    skills: [IMG_TAG],
    languages: [{ name: SCRIPT_TAG, level: 5 }],
    certificates: [SCRIPT_TAG],
    dropped: [],
  };
}

describe('ImportReview with hostile LinkedIn text', () => {
  it('renders every hostile string as text with no script or img '
    + 'element', async () => {
    const review = buildReview(hostileParsedProfile(), makeUuid());
    const choices = initialChoiceSets(review);
    const wrapper = await mountSuspended(ImportReview, {
      props: {
        copy: importCopy.en,
        createCopy: resumeCreateCopy.en,
        locale: 'en',
        review,
        dateFormat: 'Mon YYYY',
        title: 'LinkedIn resume',
        fullName: review.fullName,
        headline: review.headline,
        detailIds: choices.detailIds,
        entryIds: choices.entryIds,
        schemaCheck: { ok: true },
        disabled: false,
        titleInvalid: false,
      },
    });

    expect(wrapper.find('script').exists()).toBe(false);
    expect(wrapper.find('img').exists()).toBe(false);

    // fullName and headline render inside TextField's <input value>, which
    // is inert regardless of its content; every other hostile string renders
    // as a text node.
    const fullNameInput = wrapper
      .get<HTMLInputElement>('[data-field="fullName"] input');
    expect(fullNameInput.element.value).toBe(`Sample ${SCRIPT_TAG}`);

    const text = wrapper.text();
    expect(text).toContain(SCRIPT_TAG);
    expect(text).toContain(IMG_TAG);
    expect(text).toContain(JS_URL);
  });

  it('starts email and phone unchecked', async () => {
    const review = buildReview(hostileParsedProfile(), makeUuid());
    const choices = initialChoiceSets(review);
    const emailDetail = review.details
      .find((detail) => detail.type === 'email');
    const phoneDetail = review.details
      .find((detail) => detail.type === 'phone');
    expect(emailDetail).toBeDefined();
    expect(phoneDetail).toBeDefined();
    expect(choices.detailIds.has(emailDetail!.id)).toBe(false);
    expect(choices.detailIds.has(phoneDetail!.id)).toBe(false);

    const wrapper = await mountSuspended(ImportReview, {
      props: {
        copy: importCopy.en,
        createCopy: resumeCreateCopy.en,
        locale: 'en',
        review,
        dateFormat: 'Mon YYYY',
        title: 'LinkedIn resume',
        fullName: review.fullName,
        headline: review.headline,
        detailIds: choices.detailIds,
        entryIds: choices.entryIds,
        schemaCheck: { ok: true },
        disabled: false,
        titleInvalid: false,
      },
    });
    const emailRow = wrapper.findAll('li')
      .find((row) => row.text().includes(emailDetail!.value));
    const phoneRow = wrapper.findAll('li')
      .find((row) => row.text().includes(phoneDetail!.value));
    expect(emailRow?.get('[role="checkbox"]').attributes('data-state'))
      .toBe('unchecked');
    expect(phoneRow?.get('[role="checkbox"]').attributes('data-state'))
      .toBe('unchecked');
  });
});

describe('ImportReview personal details', () => {
  it('puts contactOffHint on its own line under email and phone, but not '
    + 'other details', async () => {
    const review = buildReview({
      profile: {
        fullName: 'Sample Person',
        headline: 'Product Manager',
        location: 'Hanoi, Vietnam',
      },
      contacts: [{ type: 'email', value: 'sample@example.com' }],
      work: [],
      education: [],
      skills: [],
      languages: [],
      certificates: [],
      dropped: [],
    }, makeUuid());
    const choices = initialChoiceSets(review);
    const wrapper = await mountSuspended(ImportReview, {
      props: {
        copy: importCopy.en,
        createCopy: resumeCreateCopy.en,
        locale: 'en',
        review,
        dateFormat: 'Mon YYYY',
        title: 'LinkedIn resume',
        fullName: review.fullName,
        headline: review.headline,
        detailIds: choices.detailIds,
        entryIds: choices.entryIds,
        schemaCheck: { ok: true },
        disabled: false,
        titleInvalid: false,
      },
    });

    const emailDetail = review.details.find((d) => d.type === 'email')!;
    const emailRow = wrapper.findAll('li')
      .find((row) => row.text().includes(emailDetail.value))!;
    const emailLines = emailRow.findAll('p').map((p) => p.text());
    // The field name and the off-by-default hint are separate lines, not one
    // concatenated description (docs/design/linkedin-import-ui.md,
    // "Personal details card").
    expect(emailLines).toContain('Email');
    expect(emailLines).toContain(importCopy.en.contactOffHint);

    const locationDetail = review.details.find((d) => d.type === 'location')!;
    const locationRow = wrapper.findAll('li')
      .find((row) => row.text().includes(locationDetail.value))!;
    expect(locationRow.text()).not.toContain(importCopy.en.contactOffHint);
  });

  it('marks a detail that fails the schema check and exposes its checkbox '
    + 'for focus', async () => {
    const review = buildReview({
      profile: { fullName: 'Sample Person', headline: 'Product Manager' },
      // A non-https website value fails validatePersonalDetailUrlSchemes
      // (packages/schema/validation/store.ts).
      contacts: [
        { type: 'website', value: 'javascript:alert(1)', label: 'Site' },
      ],
      work: [],
      education: [],
      skills: [],
      languages: [],
      certificates: [],
      dropped: [],
    }, makeUuid());
    const choices = initialChoiceSets(review);
    const websiteDetail = review.details[0]!;
    const wrapper = await mountSuspended(ImportReview, {
      props: {
        copy: importCopy.en,
        createCopy: resumeCreateCopy.en,
        locale: 'en',
        review,
        dateFormat: 'Mon YYYY',
        title: 'LinkedIn resume',
        fullName: review.fullName,
        headline: review.headline,
        detailIds: choices.detailIds,
        entryIds: choices.entryIds,
        schemaCheck: {
          ok: false, entryIds: [websiteDetail.id], general: false,
        },
        disabled: false,
        titleInvalid: false,
      },
    });

    const row = wrapper.findAll('li')
      .find((candidate) => candidate.text().includes(websiteDetail.value))!;
    expect(row.text()).toContain(importCopy.en.entryInvalid);
    expect(row.get('[data-import-entry]').attributes('data-import-entry'))
      .toBe(websiteDetail.id);
  });
});
