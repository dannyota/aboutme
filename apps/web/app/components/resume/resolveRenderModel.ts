import type {
  Content,
  Customization,
  PersonalDetail,
  PhotoCrop,
  Resume,
  Section,
} from '@aboutme/schema';
import { CURRENT_VERSION } from '@aboutme/schema/released';

import { type ResumeStyles, useResumeStyles } from './useResumeStyles';

export interface RenderContext {
  lng: string;
  mode: 'continuous' | 'paged';
  photoUrl?: string;
}

export type ResumeRenderErrorCode
  = | 'unsupported_schema_version'
    | 'photo_url_required'
    | 'unexpected_photo_url'
    | 'render_mode_unavailable';

export class ResumeRenderError extends Error {
  readonly code: ResumeRenderErrorCode;

  constructor(code: ResumeRenderErrorCode, message: string) {
    super(message);
    this.name = 'ResumeRenderError';
    this.code = code;
  }
}

export interface ResolvedSection {
  readonly key: string;
  readonly section: Section;
}

export interface ResolvedPhoto {
  readonly url: string;
  readonly crop?: PhotoCrop;
}

export interface ResolvedRenderModel {
  readonly personalDetails: {
    readonly fullName?: string;
    readonly headline?: string;
    readonly details: readonly PersonalDetail[];
  };
  readonly photo?: ResolvedPhoto;
  readonly main: readonly ResolvedSection[];
  readonly sidebar: readonly ResolvedSection[];
  readonly columns: 1 | 2;
  readonly header: {
    readonly align: 'left' | 'center';
    readonly detailsLayout: 'inline' | 'stacked';
    readonly iconStyle: 'none' | 'outline';
  };
  readonly heading: Customization['heading'];
  readonly sectionDisplay: Customization['sectionDisplay'];
  readonly dateFormat: Customization['dateFormat'];
  readonly pageFormat: Customization['pageFormat'];
  readonly lng: string;
  readonly mode: 'continuous';
  readonly styles: ResumeStyles;
}

const resolveSections = (
  keys: readonly string[],
  content: Content,
): ResolvedSection[] =>
  keys.flatMap((key) => {
    const section = content[key];
    return section === undefined ? [] : [{ key, section }];
  });

export function resolveRenderModel(
  document: Resume,
  context: RenderContext,
): ResolvedRenderModel {
  if (document.schemaVersion !== CURRENT_VERSION) {
    throw new ResumeRenderError(
      'unsupported_schema_version',
      `Renderer supports schema version ${CURRENT_VERSION}.`,
    );
  }
  if (context.mode === 'paged') {
    throw new ResumeRenderError(
      'render_mode_unavailable',
      'Paged rendering is not available yet.',
    );
  }

  const photoMetadata = document.personalDetails.photo;
  if (photoMetadata !== undefined && context.photoUrl === undefined) {
    throw new ResumeRenderError(
      'photo_url_required',
      'A document photo requires an authorized render-context URL.',
    );
  }
  if (photoMetadata === undefined && context.photoUrl !== undefined) {
    throw new ResumeRenderError(
      'unexpected_photo_url',
      'A photo URL is invalid when the document has no photo metadata.',
    );
  }

  const customization = document.customization;
  return {
    personalDetails: {
      ...(document.personalDetails.fullName === undefined
        ? {}
        : { fullName: document.personalDetails.fullName }),
      ...(document.personalDetails.headline === undefined
        ? {}
        : { headline: document.personalDetails.headline }),
      details: (document.personalDetails.details ?? []).filter(
        (detail) => !detail.isHidden,
      ),
    },
    ...(photoMetadata === undefined
      ? {}
      : {
          photo: {
            url: context.photoUrl!,
            ...(photoMetadata.crop === undefined
              ? {}
              : { crop: structuredClone(photoMetadata.crop) }),
          },
        }),
    main: resolveSections(customization.layout.sections.main, document.content),
    sidebar: resolveSections(
      customization.layout.sections.sidebar,
      document.content,
    ),
    columns: customization.layout.columns,
    header: {
      align: customization.header?.align ?? 'left',
      detailsLayout: customization.header?.detailsLayout ?? 'inline',
      iconStyle: customization.header?.iconStyle ?? 'outline',
    },
    heading: structuredClone(customization.heading),
    sectionDisplay: structuredClone(customization.sectionDisplay),
    dateFormat: customization.dateFormat,
    pageFormat: customization.pageFormat,
    lng: context.lng,
    mode: 'continuous',
    styles: useResumeStyles(customization),
  };
}
