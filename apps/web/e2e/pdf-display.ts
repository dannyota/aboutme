import type { PDFPageProxy } from 'pdfjs-dist/legacy/build/pdf.mjs';

export interface PdfTextItem {
  readonly text: string;
  readonly x: number;
  readonly y: number;
  readonly width: number;
  readonly height: number;
}

export interface PdfTextPage {
  readonly width: number;
  readonly height: number;
  readonly text: string;
  readonly items: readonly PdfTextItem[];
}

export interface PdfMembership {
  readonly pageCount: number;
  readonly pages: readonly {
    readonly main: {
      readonly first: string | null;
      readonly last: string | null;
    };
    readonly sidebar: {
      readonly first: string | null;
      readonly last: string | null;
    };
    readonly start: boolean;
    readonly end: boolean;
    readonly opposite: boolean;
  }[];
}

function orderedText(page: PdfTextPage): string {
  return [...page.items]
    .sort((a, b) => b.y - a.y || a.x - b.x)
    .map((item) => item.text)
    .join(' ')
    .replace(/-\s+(?=ENTRY-\d{2})/g, '-')
    .replace(/\s+/g, ' ');
}

export function pdfEntryIds(
  pages: readonly PdfTextPage[],
  column: 'MAIN' | 'SIDEBAR',
): string[] {
  const pattern = new RegExp(`${column}-ENTRY-\\d{2}`, 'g');
  return pages.flatMap((page) =>
    [...orderedText(page).matchAll(pattern)].map((match) => match[0]),
  );
}

export function pdfMembership(
  pages: readonly PdfTextPage[],
  markers: {
    readonly start: string;
    readonly end: string;
    readonly opposite: string;
  },
): PdfMembership {
  return {
    pageCount: pages.length,
    pages: pages.map((page) => {
      const text = orderedText(page);
      const column = (name: 'MAIN' | 'SIDEBAR') => {
        const ids = pdfEntryIds([page], name);
        return { first: ids[0] ?? null, last: ids.at(-1) ?? null };
      };
      return {
        main: column('MAIN'),
        sidebar: column('SIDEBAR'),
        start: text.includes(markers.start),
        end: text.includes(markers.end),
        opposite: text.includes(markers.opposite),
      };
    }),
  };
}

export async function pdfTextPage(page: PDFPageProxy): Promise<PdfTextPage> {
  const content = await page.getTextContent();
  const items = content.items.flatMap((item): PdfTextItem[] => {
    if (!('str' in item) || item.str.trim() === '') return [];
    return [{
      text: item.str.normalize('NFC'),
      x: item.transform[4]!,
      y: item.transform[5]!,
      width: item.width,
      height: item.height,
    }];
  });
  const viewport = page.getViewport({ scale: 1 });
  return {
    width: viewport.width,
    height: viewport.height,
    text: items.map((item) => item.text).join(' ').replace(/\s+/g, ' '),
    items,
  };
}

export function pdfMarkerPosition(
  page: PdfTextPage,
  marker: string,
): PdfTextItem | null {
  return page.items.find((item) => item.text.includes(marker)) ?? null;
}
