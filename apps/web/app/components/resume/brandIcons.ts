import { defineComponent, h } from 'vue';

// Brand marks for contact icons, drawn monochrome in the icon colour
// (docs/adr/0041-contact-link-display-and-body-justify.md). GitHub and X come
// from Simple Icons 16.31.0 (CC0-1.0); LinkedIn comes from Font Awesome Free
// 7.3.1 (CC BY 4.0). THIRD_PARTY_NOTICES.md records both sources.

const GITHUB_PATH = 'M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.42.36.81 1.096.81 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12'; // eslint-disable-line max-len

const X_PATH = 'M14.234 10.162 22.977 0h-2.072l-7.591 8.824L7.251 0H.258l9.168 13.343L.258 24H2.33l8.016-9.318L16.749 24h6.993zm-2.837 3.299-.929-1.329L3.076 1.56h3.182l5.965 8.532.929 1.329 7.754 11.09h-3.182z'; // eslint-disable-line max-len

// Font Awesome's square "linkedin" mark. The viewBox centres the square with a
// small inset so its weight matches the round GitHub mark.
const LINKEDIN_PATH = 'M416 32L31.9 32C14.3 32 0 46.5 0 64.3L0 447.7C0 465.5 14.3 480 31.9 480L416 480c17.6 0 32-14.5 32-32.3l0-383.4C448 46.5 433.6 32 416 32zM135.4 416l-66.4 0 0-213.8 66.5 0 0 213.8-.1 0zM102.2 96a38.5 38.5 0 1 1 0 77 38.5 38.5 0 1 1 0-77zM384.3 416l-66.4 0 0-104c0-24.8-.5-56.7-34.5-56.7-34.6 0-39.9 27-39.9 54.9l0 105.8-66.4 0 0-213.8 63.7 0 0 29.2 .9 0c8.9-16.8 30.6-34.5 62.9-34.5 67.2 0 79.7 44.3 79.7 101.9l0 117.2z'; // eslint-disable-line max-len

const brandIcon = (
  name: string,
  path: string,
  viewBox = '0 0 24 24',
) => defineComponent({
  name: `Brand${name}`,
  // strokeWidth is accepted so Icon.vue's Lucide prop does not fall through
  // onto a filled mark.
  props: {
    size: { type: Number, default: 24 },
    strokeWidth: { type: Number, default: 0 },
  },
  setup(props) {
    return () => h('svg', {
      xmlns: 'http://www.w3.org/2000/svg',
      width: props.size,
      height: props.size,
      viewBox,
      fill: 'currentColor',
      class: `brand-${name.toLowerCase()}`,
    }, [h('path', { d: path })]);
  },
});

export const BrandGitHub = brandIcon('GitHub', GITHUB_PATH);
export const BrandX = brandIcon('X', X_PATH);
export const BrandLinkedIn = brandIcon(
  'LinkedIn',
  LINKEDIN_PATH,
  '-14 18 476 476',
);
