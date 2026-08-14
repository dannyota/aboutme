import type {
  DateRange,
  PersonalDetail,
  Photo,
  PhotoCrop,
  Resume,
  Section,
  YearMonth,
} from '@aboutme/schema';
import type { CURRENT_VERSION } from '@aboutme/schema/released';

export type Revision = string & { readonly __revision: unique symbol };
export type ParentETag = string & { readonly __parentETag: unique symbol };

export type SaveState
  = | 'idle'
    | 'dirty'
    | 'saving'
    | 'saved'
    | 'offline'
    | 'error'
    | 'conflict'
    | 'session-lost';

export type JsonValue
  = | null
    | boolean
    | number
    | string
    | readonly JsonValue[]
    | { readonly [key: string]: JsonValue };

export type Presence<T = unknown>
  = { readonly present: false } | { readonly present: true; readonly value: T };

export interface PlacementProjection {
  readonly main: readonly string[];
  readonly sidebar: readonly string[];
}

export type TemplateCustomizationProjection = Omit<
  Resume['customization'],
  'layout'
> & {
  readonly layout: Omit<Resume['customization']['layout'], 'sections'>;
};

export interface TemplateTargetProjection {
  readonly placement: PlacementProjection;
  readonly customization: TemplateCustomizationProjection;
}

export type ContentIdentityProjection = readonly {
  readonly key: string;
  readonly sectionType: Section['sectionType'];
}[];

export type ProjectionValue
  = | JsonValue
    | ResumeSnapshot
    | Resume['personalDetails']
    | Resume['customization']
    | TemplateCustomizationProjection
    | Resume['content']
    | Section
    | Section['entries'][number]
    | ReadonlyArray<Section['entries'][number]>
    | PersonalDetail
    | readonly PersonalDetail[]
    | Photo
    | PhotoCrop
    | DateRange
    | YearMonth
    | PlacementProjection
    | TemplateTargetProjection
    | ContentIdentityProjection;

export type ProjectionContextKey
  = | 'resumeId'
    | 'schemaVersion'
    | 'ownerId'
    | 'sectionKey'
    | 'sectionType'
    | 'entryId'
    | 'membership'
    | 'photoKey'
    | 'placement'
    | 'customization'
    | 'contentIdentity'
    | `section:${string}:type`
    | `entry:${string}:membership`
    | `untouched:${string}:type`;

export interface Projection {
  readonly target: Presence<ProjectionValue>;
  readonly context: Readonly<
    Partial<Record<ProjectionContextKey, Presence<ProjectionValue>>>
  >;
}

export interface ResumeMetadata {
  readonly id: string;
  readonly title: string;
  readonly lng: string;
  readonly live: boolean;
  readonly slug: string | null;
  readonly schemaVersion: typeof CURRENT_VERSION;
  readonly createdAt: string;
  readonly updatedAt: string;
}

export interface ResumeSnapshot {
  readonly document: Resume;
  readonly metadata: ResumeMetadata;
}

export interface AcceptedResume extends ResumeSnapshot {
  readonly revision: Revision;
  readonly metadataFreshness: 'complete' | 'stale';
}

export interface EditorRuntime {
  nowEpochMs(): number;
  uuid(): string;
  delay(ms: number): Promise<void>;
}
