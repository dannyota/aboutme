import { registerEndpoint } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseHeader, setResponseStatus } from 'h3';

import { registerCapabilities } from '../support/capabilities';

export const reauthCalls: unknown[] = [];

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
  answerReauth(204, null, null);
}

export function answerReauth(
  status: number,
  contentType: string | null,
  body: string | null,
): void {
  registerEndpoint('/api/v1/auth/password/reauth', {
    method: 'POST',
    handler: (event) => {
      reauthCalls.push(event.path);
      setResponseStatus(event, status);
      if (contentType !== null) {
        setResponseHeader(event, 'Content-Type', contentType);
      }
      return body;
    },
  });
}

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
