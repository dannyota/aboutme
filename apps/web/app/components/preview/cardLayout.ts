export const CARD_LAYOUT_VERSION = 2;

export const CARD_WIDTH = 1200;

interface PreviewCardCrop {
  x: number;
  y: number;
  width: number;
  height: number;
}

interface PreviewCardPhoto {
  url: string;
  crop: PreviewCardCrop | null;
}

export interface PreviewCardContent {
  layoutVersion: number;
  lng: string;
  slug: string;
  name: string | null;
  headline: string | null;
  photo: PreviewCardPhoto | null;
  accent: string;
}
