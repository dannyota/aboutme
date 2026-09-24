import type { RenderContext } from '../components/resume/resolveRenderModel';

export const sampleContext: RenderContext = {
  lng: 'en',
  mode: 'continuous',
  photoUrl: '/landing/danny.jpg',
  // The homepage hero owns the page's one h1; the embedded sample's name
  // is visual only.
  nameHeading: 'p',
};
