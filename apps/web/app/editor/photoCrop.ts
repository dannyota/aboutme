import type { PhotoCrop } from '@aboutme/schema';

/**
 * Square photo crops. A crop is stored normalized to the image (0–1 on each
 * axis); a square crop is square in pixels, so its normalized width and height
 * differ unless the image is square. Zoom 1 is the largest square that fits.
 */

export const MIN_ZOOM = 1;
export const MAX_ZOOM = 4;

export interface ImageSize {
  readonly width: number;
  readonly height: number;
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

function round(value: number): number {
  return Math.round(value * 1_000_000) / 1_000_000;
}

/** The side of the crop square in pixels at `zoom`. */
function sidePx(image: ImageSize, zoom: number): number {
  return Math.min(image.width, image.height) / clamp(zoom, MIN_ZOOM, MAX_ZOOM);
}

/** A square crop of `zoom`, centred on (cx, cy) in 0–1 image coordinates. */
export function squareCrop(
  image: ImageSize,
  zoom: number,
  cx: number,
  cy: number,
): PhotoCrop {
  const side = sidePx(image, zoom);
  const width = side / image.width;
  const height = side / image.height;
  return {
    x: round(clamp(cx - width / 2, 0, 1 - width)),
    y: round(clamp(cy - height / 2, 0, 1 - height)),
    width: round(width),
    height: round(height),
  };
}

/**
 * The starting crop for a new photo: the largest square, centred across and,
 * on a portrait photo, centred on the top third, where a face usually is.
 */
export function defaultCrop(image: ImageSize): PhotoCrop {
  const portrait = image.height > image.width;
  return squareCrop(image, MIN_ZOOM, 0.5, portrait ? 1 / 3 : 0.5);
}

/** The zoom a crop represents, from its pixel width. */
export function zoomOf(image: ImageSize, crop: PhotoCrop): number {
  const side = crop.width * image.width;
  if (side <= 0) return MIN_ZOOM;
  return clamp(
    Math.min(image.width, image.height) / side,
    MIN_ZOOM,
    MAX_ZOOM,
  );
}

/** The crop's centre in 0–1 image coordinates. */
export function centreOf(crop: PhotoCrop): { cx: number; cy: number } {
  return { cx: crop.x + crop.width / 2, cy: crop.y + crop.height / 2 };
}

/** Re-zooms around the current centre, keeping the crop inside the image. */
export function withZoom(
  image: ImageSize,
  crop: PhotoCrop,
  zoom: number,
): PhotoCrop {
  const { cx, cy } = centreOf(crop);
  return squareCrop(image, zoom, cx, cy);
}

/** Moves the crop by a fraction of the image, stopping at the edges. */
export function movedBy(crop: PhotoCrop, dx: number, dy: number): PhotoCrop {
  return {
    ...crop,
    x: round(clamp(crop.x + dx, 0, 1 - crop.width)),
    y: round(clamp(crop.y + dy, 0, 1 - crop.height)),
  };
}

/** Whether a crop lies inside the image with a positive size. */
export function isValidCrop(crop: PhotoCrop): boolean {
  const values = [crop.x, crop.y, crop.width, crop.height];
  return values.every(Number.isFinite)
    && crop.x >= 0 && crop.y >= 0
    && crop.width > 0 && crop.height > 0
    && crop.x + crop.width <= 1 + 1e-9
    && crop.y + crop.height <= 1 + 1e-9;
}

/**
 * How to draw a crop in a square frame: the image scaled so the crop fills
 * the frame and shifted so the crop's corner sits at the frame's corner. The
 * renderer uses the same model, so the preview matches the resume.
 */
export function cropImageStyle(crop: PhotoCrop): Record<string, string> {
  return {
    position: 'absolute',
    width: `${round(100 / crop.width)}%`,
    height: `${round(100 / crop.height)}%`,
    left: `${round((-crop.x / crop.width) * 100)}%`,
    top: `${round((-crop.y / crop.height) * 100)}%`,
    maxWidth: 'none',
  };
}
