import { defineComponent, h, type PropType } from 'vue';

/**
 * The project's description: the library's stored rich text, which Go
 * sanitized on write. Go's validator requires this element's content to be
 * exactly that text (docs/design/projects-contract.md "Render envelope"), so
 * it is written as given and never parsed into new elements here. This is the
 * project pages' only raw HTML. A render function keeps the template free of
 * lint directives, which the development compiler would write into the page
 * and so break its parity with the production worker.
 */
export default defineComponent({
  name: 'ProjectDescription',
  props: {
    html: { type: String, required: true },
    lang: { type: String as PropType<string | undefined>, default: undefined },
  },
  setup(props) {
    return () => h('div', {
      class: 'project-description',
      lang: props.lang,
      innerHTML: props.html,
    });
  },
});
