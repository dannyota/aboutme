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
