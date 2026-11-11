import { registerEndpoint } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseStatus } from 'h3';

import { registerCapabilities } from '../support/capabilities';

export const reauthCalls: unknown[] = [];

/** A signed-in session with a password, as the pages read it. */
export function registerSignedIn(): void {
  registerCapabilities({ providerLogin: false, agentAccess: false });
  registerEndpoint('/api/v1/me', () => ({
    data: {
      user: {
        id: 'owner-1',
        email: 'owner@example.invalid',
        name: 'Owner',
        avatarKey: null,
        hasPassword: true,
      },
      csrfToken: 'csrf-1',
      identities: [],
    },
  }));
  registerEndpoint('/api/v1/auth/password/reauth', {
    method: 'POST',
    handler: (event) => {
      reauthCalls.push(event.path);
      setResponseStatus(event, 204);
      return null;
    },
  });
}

/** Elements in dialogs render outside the mounted wrapper. */
export function inDocument<T extends Element = HTMLElement>(
  selector: string,
): T {
  const element = document.body.querySelector<T>(selector);
  if (element === null) throw new Error(`Missing ${selector}`);
  return element;
}

export async function typeInto(selector: string, value: string): Promise<void> {
  const input = inDocument<HTMLInputElement>(selector);
  input.value = value;
  input.dispatchEvent(new Event('input', { bubbles: true }));
  await flushPromises();
}

export async function submitDialog(): Promise<void> {
  inDocument('[role="dialog"] form').dispatchEvent(
    new Event('submit', { bubbles: true, cancelable: true }),
  );
  await flushPromises();
}

export async function click(selector: string): Promise<void> {
  inDocument(selector).click();
  await flushPromises();
}

export function dialogText(): string {
  return document.body.querySelector('[role="dialog"]')?.textContent ?? '';
}
