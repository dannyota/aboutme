import type { InjectionKey } from 'vue';

export type Announce = (message: string) => void;

export const verifyAnnounceKey: InjectionKey<Announce>
  = Symbol('verify-announce');
