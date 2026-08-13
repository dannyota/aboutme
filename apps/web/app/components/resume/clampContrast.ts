interface RGB {
  r: number;
  g: number;
  b: number;
}

interface OKLCH {
  l: number;
  c: number;
  h: number;
}

const parseHex = (color: string): RGB => {
  if (!/^#[0-9a-f]{6}$/i.test(color)) {
    throw new Error(`invalid color: ${color}`);
  }
  return {
    r: Number.parseInt(color.slice(1, 3), 16) / 255,
    g: Number.parseInt(color.slice(3, 5), 16) / 255,
    b: Number.parseInt(color.slice(5, 7), 16) / 255,
  };
};

const channelToLinear = (value: number): number =>
  value <= 0.04045
    ? value / 12.92
    : ((value + 0.055) / 1.055) ** 2.4;

const channelFromLinear = (value: number): number =>
  value <= 0.0031308
    ? 12.92 * value
    : 1.055 * value ** (1 / 2.4) - 0.055;

const toHex = ({ r, g, b }: RGB): string => {
  const component = (value: number): string =>
    Math.round(Math.min(1, Math.max(0, value)) * 255)
      .toString(16)
      .padStart(2, '0');
  return `#${component(r)}${component(g)}${component(b)}`;
};

const relativeLuminance = (color: string): number => {
  const { r, g, b } = parseHex(color);
  return (
    0.2126 * channelToLinear(r)
    + 0.7152 * channelToLinear(g)
    + 0.0722 * channelToLinear(b)
  );
};

export const contrastRatio = (first: string, second: string): number => {
  const lighter = Math.max(
    relativeLuminance(first),
    relativeLuminance(second),
  );
  const darker = Math.min(
    relativeLuminance(first),
    relativeLuminance(second),
  );
  return (lighter + 0.05) / (darker + 0.05);
};

const minimumContrast = (color: string, surfaces: readonly string[]): number =>
  Math.min(...surfaces.map((surface) => contrastRatio(color, surface)));

const rgbToOKLCH = (color: string): OKLCH => {
  const rgb = parseHex(color);
  const r = channelToLinear(rgb.r);
  const g = channelToLinear(rgb.g);
  const b = channelToLinear(rgb.b);
  const l = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b);
  const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b);
  const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b);
  const labA = 1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s;
  const labB = 0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s;
  return {
    l: 0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s,
    c: Math.hypot(labA, labB),
    h: Math.atan2(labB, labA),
  };
};

const oklchToHex = ({ l, c, h }: OKLCH): string => {
  const labA = c * Math.cos(h);
  const labB = c * Math.sin(h);
  const lPrime = (l + 0.3963377774 * labA + 0.2158037573 * labB) ** 3;
  const mPrime = (l - 0.1055613458 * labA - 0.0638541728 * labB) ** 3;
  const sPrime = (l - 0.0894841775 * labA - 1.291485548 * labB) ** 3;
  return toHex({
    r: channelFromLinear(
      4.0767416621 * lPrime - 3.3077115913 * mPrime + 0.2309699292 * sPrime,
    ),
    g: channelFromLinear(
      -1.2684380046 * lPrime + 2.6097574011 * mPrime - 0.3413193965 * sPrime,
    ),
    b: channelFromLinear(
      -0.0041960863 * lPrime - 0.7034186147 * mPrime + 1.707614701 * sPrime,
    ),
  });
};

export function clampAgainst(
  color: string,
  surfaces: readonly string[],
  target: number,
): string | null {
  if (surfaces.length === 0) {
    throw new Error('at least one surface is required');
  }
  if (minimumContrast(color, surfaces) >= target) return color;

  const blackScore = minimumContrast('#000000', surfaces);
  const whiteScore = minimumContrast('#ffffff', surfaces);
  const endpoint = blackScore >= whiteScore ? '#000000' : '#ffffff';
  if (minimumContrast(endpoint, surfaces) < target) return null;

  const source = rgbToOKLCH(color);
  const endpointL = endpoint === '#000000' ? 0 : 1;
  const direction = endpointL > source.l ? 1 : -1;
  for (
    let lightness = source.l + direction * 0.005;
    direction > 0 ? lightness < endpointL : lightness > endpointL;
    lightness += direction * 0.005
  ) {
    const candidate = oklchToHex({ ...source, l: lightness });
    if (minimumContrast(candidate, surfaces) >= target) return candidate;
  }
  return endpoint;
}

export const mixInSRGB = (
  color: string,
  surface: string,
  surfaceWeight: number,
): string => {
  const source = parseHex(color);
  const target = parseHex(surface);
  return toHex({
    r: source.r * (1 - surfaceWeight) + target.r * surfaceWeight,
    g: source.g * (1 - surfaceWeight) + target.g * surfaceWeight,
    b: source.b * (1 - surfaceWeight) + target.b * surfaceWeight,
  });
};

export function deriveLevelColors(
  accent: string,
  surface: string,
): { solid: string; track: string } {
  let track = mixInSRGB(accent, surface, 0.8);
  let solid = clampAgainst(accent, [surface, track], 3);
  if (solid !== null) return { solid, track };

  track = surface;
  solid = clampAgainst(accent, [surface], 3);
  if (solid === null) {
    throw new Error('single-surface level contrast is unsatisfiable');
  }
  return { solid, track };
}
