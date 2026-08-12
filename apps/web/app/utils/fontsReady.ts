import { resolveFontSelection } from './fontCatalog';

export async function fontsReady(
  id: string,
  fonts?: FontFaceSet,
): Promise<void> {
  const selection = resolveFontSelection(id);
  const fontSet = fonts ?? document.fonts;

  for (const descriptor of selection.loadDescriptors) {
    const loaded = await fontSet.load(descriptor);
    if (loaded.length === 0) {
      throw new Error(`nothing loaded for font descriptor: ${descriptor}`);
    }
  }

  await fontSet.ready;
}
