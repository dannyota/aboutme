/**
 * PublicResume rich text has already passed the versioned Go sanitizer before
 * projection. Re-sanitizing it in the isolated renderer would import the
 * editor's hostile corpus and break the renderer's closed artifact boundary.
 */
export const sanitizeRichText = (html: string): string => html;
