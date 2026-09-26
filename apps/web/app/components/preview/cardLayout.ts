/**
 * The link-preview card layout (docs/design/link-previews.md, "Preview
 * card"; docs/adr/0055-stored-link-preview-card.md).
 *
 * CARD_LAYOUT_VERSION equals the server's card layout version. The server
 * hashes it into every card version, and a test pins a hash of the card
 * markup and CSS to it, so a layout change raises it on both sides and every
 * live card gets a new URL.
 */
export const CARD_LAYOUT_VERSION = 1;

export const CARD_WIDTH = 1200;
export const CARD_HEIGHT = 630;

export interface PreviewCardCrop {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface PreviewCardPhoto {
  /**
   * A data:image/jpeg or data:image/png URL in print; the editor's photo
   * URL in the publish dialog.
   */
  url: string;
  /** Fractions of the image; null shows the whole image. */
  crop: PreviewCardCrop | null;
}

/** Everything the card shows; no contact, section, or document field. */
export interface PreviewCardContent {
  layoutVersion: number;
  /** Canonical BCP 47 language, or und. */
  lng: string;
  slug: string;
  /** Normalized name, or null; with no name the card shows the address. */
  name: string | null;
  /** Normalized headline, or null; always null when name is null. */
  headline: string | null;
  photo: PreviewCardPhoto | null;
  /** #rrggbb, already clamped to 3:1 against white. */
  accent: string;
}
