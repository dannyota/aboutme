import { defineComponent, h, type PropType } from 'vue';

// h(), not a template: Vue dev builds render a template's lint comments.
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
