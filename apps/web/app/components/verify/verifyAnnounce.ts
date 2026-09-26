// The verify page's shared status announcer (docs/design/deployment-
// transparency/visual.md, "Digest chip and copy"): a page-level visually
// hidden `role="status"` region, separate from the status card, that
// confirms a copy action. The page provides the announce function; every
// copy button injects it instead of holding its own live region.
import type { InjectionKey } from 'vue';

export type Announce = (message: string) => void;

export const verifyAnnounceKey: InjectionKey<Announce>
  = Symbol('verify-announce');
