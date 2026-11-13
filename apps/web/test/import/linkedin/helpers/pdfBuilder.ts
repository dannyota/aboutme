import { deflateSync } from 'node:zlib';

const encoder = new TextEncoder();

export type PdfObject = string | { dict: string; stream: Uint8Array };

function concat(parts: readonly Uint8Array[]): Uint8Array {
  const total = parts.reduce((sum, part) => sum + part.length, 0);
  const out = new Uint8Array(total);
  let offset = 0;
  for (const part of parts) {
    out.set(part, offset);
    offset += part.length;
  }
  return out;
}

export function buildPdf(
  objects: readonly PdfObject[],
  trailer = '/Root 1 0 R',
): Uint8Array {
  const parts: Uint8Array[] = [encoder.encode('%PDF-1.4\n')];
  let length = parts[0]!.length;
  const offsets: number[] = [];
  objects.forEach((object, index) => {
    offsets.push(length);
    const body = typeof object === 'string'
      ? encoder.encode(`${index + 1} 0 obj\n${object}\nendobj\n`)
      : concat([
          encoder.encode(
            `${index + 1} 0 obj\n<< ${object.dict} `
            + `/Length ${object.stream.length} >>\nstream\n`,
          ),
          object.stream,
          encoder.encode('\nendstream\nendobj\n'),
        ]);
    parts.push(body);
    length += body.length;
  });
  const xref = [
    'xref',
    `0 ${objects.length + 1}`,
    '0000000000 65535 f ',
    ...offsets.map((offset) => `${String(offset).padStart(10, '0')} 00000 n `),
    'trailer',
    `<< /Size ${objects.length + 1} ${trailer} >>`,
    'startxref',
    String(length),
    '%%EOF',
    '',
  ].join('\n');
  parts.push(encoder.encode(xref));
  return concat(parts);
}

export function stream(content: string, compress = false):
{ dict: string; stream: Uint8Array } {
  const bytes = encoder.encode(content);
  return compress
    ? { dict: '/Filter /FlateDecode', stream: deflateSync(bytes) }
    : { dict: '', stream: bytes };
}

function pdfString(text: string): string {
  return `(${text.replace(/[\\()]/g, (c) => `\\${c}`)})`;
}

export interface TextLineSpec {
  text: string;
  x: number;
  y: number;
  size: number;
}

export interface TextPdfOptions {
  extraCatalog?: string;
  extraObjects?: readonly PdfObject[];
  mediaBox?: string;
}

// pdf.js maps the standard Helvetica font to Unicode without loading it.
export function textPdf(
  pages: readonly (readonly TextLineSpec[])[],
  options: TextPdfOptions = {},
): Uint8Array {
  const pageCount = pages.length;
  const firstPage = 4;
  const kids = pages.map((_, i) => `${firstPage + i * 2} 0 R`).join(' ');
  const objects: PdfObject[] = [
    `<< /Type /Catalog /Pages 2 0 R ${options.extraCatalog ?? ''} >>`,
    `<< /Type /Pages /Kids [${kids}] /Count ${pageCount} >>`,
    '<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica '
    + '/Encoding /WinAnsiEncoding >>',
  ];
  pages.forEach((lines, i) => {
    objects.push(
      `<< /Type /Page /Parent 2 0 R `
      + `/MediaBox ${options.mediaBox ?? '[0 0 612 792]'} `
      + '/Resources << /Font << /F1 3 0 R >> >> '
      + `/Contents ${firstPage + i * 2 + 1} 0 R >>`,
    );
    const ops = lines.map((line) =>
      `BT /F1 ${line.size} Tf 1 0 0 1 ${line.x} ${line.y} Tm `
      + `${pdfString(line.text)} Tj ET`,
    ).join('\n');
    objects.push(stream(ops));
  });
  objects.push(...(options.extraObjects ?? []));
  return buildPdf(objects);
}

export function pdfBlob(bytes: Uint8Array): Blob {
  return new Blob([bytes], { type: 'application/pdf' });
}
