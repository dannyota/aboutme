import { createSSRApp, h, type Component } from 'vue';
import { renderToString } from 'vue/server-renderer';

// eslint-disable-next-line max-len
import ProjectListPage from '../../../app/components/public/ProjectListPage.vue';
import ProjectPage from '../../../app/components/public/ProjectPage.vue';
import {
  escapeAttribute,
  escapeText,
  previewHead,
  type PublicAssetVersions,
  validAssetVersion,
} from './page-head';
import {
  PUBLIC_RENDER_FAILURE,
  PUBLIC_RENDER_HTML_MAX_BYTES,
} from '../../utils/public-render/envelope';
import type {
  PublicRenderProjectListRequest,
  PublicRenderProjectRequest,
} from '../../utils/public-render/project-envelope';

type ProjectRequest
  = | PublicRenderProjectListRequest
    | PublicRenderProjectRequest;

const document = async (
  request: ProjectRequest,
  versions: PublicAssetVersions,
  page: Component,
  props: Record<string, unknown>,
): Promise<string> => {
  try {
    if (!validAssetVersion(versions.style)) throw new Error();
    const { resume, preview } = request;
    const body = await renderToString(
      createSSRApp({ render: () => h(page, props) }),
    );
    const imageURL = preview.imageUrl ?? [
      request.canonicalOrigin,
      '/api/v1/public/resumes/',
      resume.slug,
      '/og.png',
    ].join('');
    const head = [
      '<meta charset="utf-8">',
      '<meta name="viewport" content="width=device-width, initial-scale=1">',
      // Safari auto-links digit runs such as date ranges as tel: links.
      '<meta name="format-detection" '
      + 'content="telephone=no, date=no, address=no, email=no">',
      `<title>${escapeText(request.pageTitle)}</title>`,
      request.faviconHref === ''
        ? ''
        : `<link rel="icon" href="${escapeAttribute(request.faviconHref)}">`,
      `<link rel="canonical" href="${escapeAttribute(request.pageUrl)}">`,
      previewHead(request.pageUrl, preview, imageURL, 'website'),
      '<link rel="stylesheet" ',
      `href="/_nuxt/assets/print-fonts.css?v=${versions.style}">`,
      '<link rel="stylesheet" ',
      `href="/_nuxt/assets/project-pages.css?v=${versions.style}">`,
    ].join('');
    const html = [
      '<!doctype html>',
      `<html lang="${escapeAttribute(resume.lng)}" `,
      `data-scheme="${resume.colorScheme}"><head>${head}</head>`,
      `<body>${body}</body></html>`,
    ].join('');
    if (Buffer.byteLength(html, 'utf8') > PUBLIC_RENDER_HTML_MAX_BYTES) {
      throw new Error();
    }
    return html;
  } catch {
    throw new Error(PUBLIC_RENDER_FAILURE);
  }
};

export async function renderPublicProjectList(
  request: PublicRenderProjectListRequest,
  versions: PublicAssetVersions,
): Promise<string> {
  return document(request, versions, ProjectListPage, {
    resume: request.resume,
    projects: request.projects,
    homeHref: `${request.canonicalOrigin}/`,
  });
}

export async function renderPublicProject(
  request: PublicRenderProjectRequest,
  versions: PublicAssetVersions,
): Promise<string> {
  return document(request, versions, ProjectPage, {
    resume: request.resume,
    project: request.project,
    ...(request.entryLink === undefined
      ? {}
      : { entryLink: request.entryLink }),
    listUrl: request.listUrl,
    otherProjects: request.otherProjects,
    homeHref: `${request.canonicalOrigin}/`,
  });
}
