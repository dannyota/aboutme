import type { Customization, DateRange } from '@aboutme/schema';

import type { PublicRenderPreview } from './envelope';

/**
 * Renderer inputs for a resume's project list and project pages
 * (docs/design/projects-contract.md "Render envelope"). Go builds both from
 * one admitted snapshot; they carry no project id, entry id, account data,
 * contact detail, photo or other resume content.
 */

/** Everything a project page shows of its resume. */
export interface ProjectPageResume {
  slug: string;
  /** The resume page's absolute address. */
  url: string;
  /** The full name, or the effective page title when the name is blank. */
  ownerName: string;
  /** The resume language tag, for the document's lang attribute. */
  lng: string;
  /** The language of the page's own copy. */
  uiLng: 'vi' | 'en';
  /** The resume's public color scheme, 'light' when unset. */
  colorScheme: NonNullable<Customization['colorScheme']>;
  dateFormat: Customization['dateFormat'];
}

interface ProjectTextFields {
  slug: string;
  /** The project page's absolute address. */
  url: string;
  /** Present only when the text is not in the resume's language. */
  lang?: string;
  name: string;
  role?: string;
  dates?: DateRange;
  summary?: string;
  technologies: string[];
}

/** One row of the project list. */
export interface ProjectSummary extends ProjectTextFields {
  /** At most the first six names; moreTechnologies counts the rest. */
  technologies: string[];
  /** Absent when the row shows every name. */
  moreTechnologies?: number;
}

/** The project a project page shows. */
export interface ProjectDetail extends ProjectTextFields {
  outcome?: string;
  /** Stored rich text, sanitized on write. */
  description?: string;
  repositoryUrl?: string;
  demoUrl?: string;
}

interface ProjectRenderCommon {
  canonicalOrigin: string;
  /** The page's own absolute address: canonical link and og:url. */
  pageUrl: string;
  discoveryEnabled: boolean;
  pageTitle: string;
  /** The resume's favicon, or '' when the owner set no icon. */
  faviconHref: string;
  /**
   * This page's title and description, and the resume's locale, card image
   * and alt text. An empty description means the page has none.
   */
  preview: PublicRenderPreview;
  resume: ProjectPageResume;
}

export interface PublicRenderProjectListRequest extends ProjectRenderCommon {
  mode: 'project-list';
  /** In the resume's layout and entry order. */
  projects: ProjectSummary[];
}

export interface PublicRenderProjectRequest extends ProjectRenderCommon {
  mode: 'project';
  project: ProjectDetail;
  /** The entry's own https link, only when it differs from both buttons. */
  entryLink?: string;
  /** The project list's absolute address. */
  listUrl: string;
  /** Whether the resume has another available project. */
  otherProjects: boolean;
}
