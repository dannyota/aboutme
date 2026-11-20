<script lang="ts">
import type { Resume } from '@aboutme/schema';
import {
  type Component,
  computed,
  defineComponent,
  h,
  onMounted,
  type PropType,
  provide,
  ref,
} from 'vue';

import type { components } from '../../api/generated/openapi';
import { ResumeLngKey } from '../resume/formatDate';
import LayoutColumns from '../resume/LayoutColumns.vue';
import { resolvePageGeometry } from '../resume/pageMetrics';
import {
  NO_PROJECT_LINKS,
  type ProjectLinkMap,
  ProjectLinksKey,
} from '../resume/projectLinks';
import {
  type RenderContext,
  resolveRenderModel,
} from '../resume/resolveRenderModel';
import ResumeHeader from '../resume/ResumeHeader.vue';
import { articleStyle } from '../resume/useResumeStyles';
import PublicToolbar from './PublicToolbar.vue';

type PublicResume = components['schemas']['PublicResume'];

export default defineComponent({
  name: 'PublicResumeApp',
  props: {
    publicResume: { type: Object as PropType<PublicResume>, required: true },
    homeHref: { type: String, required: true },
    projectsUrl: { type: String, default: undefined },
    projectLinks: {
      type: Object as PropType<ProjectLinkMap>,
      default: undefined,
    },
    pages: { type: Object as PropType<Component>, default: undefined },
  },
  setup(props) {
    const document = computed(() => {
      const source = props.publicResume.document;
      return {
        ...source,
        personalDetails: {
          ...source.personalDetails,
          ...(source.personalDetails.photo === undefined
            ? {}
            : {
                photo: {
                  ...source.personalDetails.photo,
                  key: 'public-render-photo',
                },
              }),
          ...(source.personalDetails.details === undefined
            ? {}
            : {
                details: source.personalDetails.details.map((detail) => ({
                  ...detail,
                  isHidden: false,
                })),
              }),
        },
      } as unknown as Resume;
    });

    const colorScheme = computed<'dark' | 'system' | undefined>(() => {
      const scheme = props.publicResume.document.customization.colorScheme;
      return scheme === 'dark' || scheme === 'system' ? scheme : undefined;
    });

    const photoContext = computed(() =>
      props.publicResume.document.personalDetails.photo === undefined
        ? {}
        : { photoUrl: props.publicResume.document.personalDetails.photo.url });
    const model = computed(() => resolveRenderModel(document.value, {
      lng: props.publicResume.lng,
      mode: 'continuous',
      ...photoContext.value,
    }));
    provide(ResumeLngKey, computed(() => model.value.lng));
    const projectLinks = computed(() => props.projectLinks ?? NO_PROJECT_LINKS);
    provide(ProjectLinksKey, projectLinks);
    const rootStyle = computed(() => articleStyle(model.value.styles.root));
    const pagedContext = computed((): RenderContext => ({
      lng: props.publicResume.lng,
      mode: 'paged',
      ...photoContext.value,
      ...(props.projectLinks === undefined
        ? {}
        : { projectLinks: props.projectLinks }),
    }));

    const vietnamese = computed(() =>
      props.publicResume.lng.split('-')[0]?.toLowerCase() === 'vi');
    const showProjects = computed(() =>
      props.projectsUrl !== undefined && projectLinks.value.size > 0);

    const mounted = ref(false);
    const pagesState = ref<string>();
    onMounted(() => {
      mounted.value = true;
    });
    const pageWidth = computed(() => mounted.value
      ? {
          '--public-page-width':
            `${resolvePageGeometry(document.value.customization).widthPx}px`,
        }
      : undefined);

    return () => h('div', {
      'class': 'public-resume-page',
      'data-color-scheme': colorScheme.value,
      'data-pages': pagesState.value,
      ...(pageWidth.value === undefined ? {} : { style: pageWidth.value }),
    }, [
      h(PublicToolbar, {
        homeHref: props.homeHref,
        creditLabel: vietnamese.value
          ? 'Tạo bằng aboutme.vn'
          : 'Built with aboutme.vn',
        showProjects: showProjects.value,
        projectsUrl: props.projectsUrl,
        projectsLabel: vietnamese.value ? 'Dự án' : 'Projects',
        downloadEnabled: props.publicResume.downloadEnabled,
        downloadHref: `/api/v1/public/resumes/${props.publicResume.slug}/pdf`,
        downloadLabel: vietnamese.value ? 'Tải PDF' : 'Download PDF',
      }),
      h('article', {
        class: 'resume-document',
        lang: model.value.lng,
        style: rootStyle.value,
      }, [
        h('div', { style: model.value.styles.header }, [h(ResumeHeader, {
          personalDetails: model.value.personalDetails,
          header: model.value.header,
          photo: model.value.photo,
          nameHeading: model.value.nameHeading,
        })]),
        h(LayoutColumns, { model: model.value }),
      ]),
      ...(props.pages === undefined || !mounted.value
        ? []
        : [h(props.pages, {
            document: document.value,
            context: pagedContext.value,
            onState: (state: string) => {
              pagesState.value = state;
            },
          })]),
    ]);
  },
});
</script>
