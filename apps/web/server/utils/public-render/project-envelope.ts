import type { Customization, DateRange } from '@aboutme/schema';

import type { PublicRenderPreview } from './envelope';

export interface ProjectPageResume {
  slug: string;
  url: string;
  ownerName: string;
  lng: string;
  uiLng: 'vi' | 'en';
  colorScheme: NonNullable<Customization['colorScheme']>;
  dateFormat: Customization['dateFormat'];
}

interface ProjectTextFields {
  slug: string;
  url: string;
  lang?: string;
  name: string;
  role?: string;
  dates?: DateRange;
  summary?: string;
  technologies: string[];
}

export interface ProjectSummary extends ProjectTextFields {
  technologies: string[];
  moreTechnologies?: number;
}

export interface ProjectDetail extends ProjectTextFields {
  outcome?: string;
  description?: string;
  repositoryUrl?: string;
  demoUrl?: string;
}

interface ProjectRenderCommon {
  canonicalOrigin: string;
  pageUrl: string;
  discoveryEnabled: boolean;
  pageTitle: string;
  faviconHref: string;
  preview: PublicRenderPreview;
  resume: ProjectPageResume;
}

export interface PublicRenderProjectListRequest extends ProjectRenderCommon {
  mode: 'project-list';
  projects: ProjectSummary[];
}

export interface PublicRenderProjectRequest extends ProjectRenderCommon {
  mode: 'project';
  project: ProjectDetail;
  entryLink?: string;
  listUrl: string;
  otherProjects: boolean;
}
