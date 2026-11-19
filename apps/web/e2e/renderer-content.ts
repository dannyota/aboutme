import { expect, type Page } from '@playwright/test';
import type { Resume } from '@aboutme/schema';
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const WRAPPED_PROFILE = 'Kỹ sư giàu kinh nghiệm với hơn mười năm xây dựng '
  + 'hệ thống phân tán cho ngân hàng, thương mại điện tử và dịch vụ công. '
  + 'Tôi tập trung vào độ tin cậy, khả năng quan sát và trải nghiệm của nhà '
  + 'phát triển, đồng thời hướng dẫn đội ngũ áp dụng kiểm thử tự động, '
  + 'triển khai liên tục và đánh giá thiết kế định kỳ để sản phẩm dễ sử '
  + 'dụng và dễ bảo trì.';
const WRAPPED_WORK = [
  'Dẫn dắt thiết kế nền tảng dữ liệu phục vụ hàng triệu người dùng mỗi ngày, '
  + 'giảm độ trễ truy vấn trung bình xuống còn một phần ba so với '
  + 'hệ thống cũ.',
  'Hướng dẫn đội ngũ kỹ sư xây dựng quy trình đánh giá mã nguồn, giám sát '
  + 'sản xuất và phản hồi sự cố trong vòng mười lăm phút.',
];

export async function expectRendererContent(
  page: Page, language: 'vi' | 'en', justify = false,
  template = 'modern-sidebar',
): Promise<void> {
  const fixture = language === 'vi' ? 'vn-full' : 'full';
  const resume = JSON.parse(await readFile(resolve(import.meta.dirname,
    `../../../packages/schema/fixtures/${fixture}.json`), 'utf8')) as Resume;
  const roots = page.locator('.resume-document:not(.pagination-measurement)');
  await expect(roots.locator('.resume-headline'), 'Renderer headline content.')
    .toHaveText(resume.personalDetails.headline!);
  const fragments: string[] = [];
  for (const [key, section] of Object.entries(resume.content)) {
    for (const entry of section.entries.filter((item) => !item.isHidden)) {
      if (justify && key === 'profile') {
        fragments.push(WRAPPED_PROFILE);
        continue;
      }
      if (justify && key === 'work') {
        fragments.push(...WRAPPED_WORK);
        continue;
      }
      const html = 'text' in entry
        ? entry.text
        : 'description' in entry
          ? entry.description
          : 'infoHtml' in entry
            ? entry.infoHtml
            : '';
      const blocks = (html ?? '').match(/<(p|li)>[\s\S]*?<\/\1>/gu) ?? [];
      fragments.push(...blocks.map((block) =>
        block.replace(/<[^>]+>/gu, '').normalize('NFC')));
    }
  }
  const bodies = roots.locator('.entry-body :is(p,li):not(:has(p,li))');
  await expect(bodies, 'Renderer body content.').toHaveCount(fragments.length);
  for (const text of fragments) {
    await expect(bodies.filter({ hasText: text }), 'Renderer body content.')
      .toHaveText(text);
  }
  const details = roots.locator('.contact-chip');
  await expect(details, 'Renderer contact content.').toHaveCount(8);
  const defaults: Record<string, string> = {
    email: 'Email', phone: 'Phone', location: 'Location', linkedin: 'LinkedIn',
    website: 'Website', github: 'GitHub', twitter: 'X', custom: 'Detail',
  };
  for (const [index, detail] of resume.personalDetails.details.entries()) {
    const chip = details.nth(index);
    const link = /^(https:\/\/)/u.test(detail.value);
    const shown = link
      ? detail.display === 'label'
        ? detail.label || defaults[detail.type]!
        : detail.display === 'full'
          ? detail.value
          : detail.value.replace(/^https:\/\//u, '').replace(/\/$/u, '')
      : detail.value;
    const label = detail.label || defaults[detail.type]!;
    const icons = ['engineer-compact', 'modern-sidebar'].includes(template);
    const showLabel = detail.display !== 'label'
      && (!icons || Boolean(detail.label) || detail.type === 'custom');
    await expect(chip.locator('.contact-label'), 'Renderer contact label.')
      .toHaveCount(showLabel ? 1 : 0);
    if (showLabel) {
      await expect(chip.locator('.contact-label'), 'Renderer contact label.')
        .toHaveText(`${label}:`);
    }
    await expect(chip.locator(':scope > :last-child'),
      'Renderer contact content.').toHaveText(shown);
    if (link || detail.type === 'email' || detail.type === 'phone') {
      const href = link
        ? detail.value
        : detail.type === 'email'
          ? `mailto:${detail.value}`
          : `tel:${detail.value.replace(/[ .()-]/gu, '')}`;
      await expect(chip.locator('a'), 'Renderer contact link.')
        .toHaveAttribute('href', href);
    }
  }
}
