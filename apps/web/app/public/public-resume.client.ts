import { createApp, createSSRApp } from 'vue';

import type { components } from '../api/generated/openapi';
import PublicResumeApp from '../components/public/PublicResumeApp.vue';
import validatePublicResume from '#public-render-validator';

type PublicResume = components['schemas']['PublicResume'];

interface PublicResumeMounter {
  hydrate: (resume: PublicResume, root: HTMLElement) => void;
  replace: (resume: PublicResume, root: HTMLElement) => void;
}

const vueMounter: PublicResumeMounter = {
  hydrate: (publicResume, root) => {
    createSSRApp(PublicResumeApp, { publicResume }).mount(root);
  },
  replace: (publicResume, root) => {
    createApp(PublicResumeApp, { publicResume }).mount(root);
  },
};

const slugPattern = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/u;

function validPublicResume(
  value: unknown,
  slug: string,
): value is PublicResume {
  return (
    validatePublicResume(value) === true
    && (value as PublicResume).slug === slug
  );
}

const diagnostic = (reason: string): void => {
  // Public diagnostics exclude URLs, response bodies, and resume data.
  console.debug(`public resume hydration skipped: ${reason}`);
};

export async function hydratePublicResume(
  root: HTMLElement,
  slug: string,
  serverRevision: string,
  request: (slug: string) => Promise<unknown> = async (value) => {
    const response = await fetch(
      `/api/v1/public/resumes/${encodeURIComponent(value)}`,
      {
        credentials: 'omit',
      },
    );
    if (!response.ok) throw new Error('public response failed');
    const envelope = (await response.json()) as { data?: unknown };
    return envelope.data;
  },
  mounter: PublicResumeMounter = vueMounter,
): Promise<void> {
  try {
    const publicResume = await request(slug);
    if (!validPublicResume(publicResume, slug)) {
      throw new Error('invalid public response');
    }
    const original = root.innerHTML;
    if (publicResume.revision === serverRevision) {
      try {
        mounter.hydrate(publicResume, root);
      } catch {
        root.innerHTML = original;
        throw new Error('public hydration failed');
      }
      return;
    }
    try {
      root.replaceChildren();
      mounter.replace(publicResume, root);
    } catch {
      root.innerHTML = original;
      throw new Error('public mount failed');
    }
  } catch {
    diagnostic('failure');
  }
}

function boot(): void {
  const root = document.querySelector<HTMLElement>('#public-resume');
  const rawRevision = root?.dataset.revision;
  const slug = location.pathname.split('/').filter(Boolean);
  if (
    root === null
    || rawRevision === undefined
    || slug.length !== 1
    || !slugPattern.test(slug[0]!)
    || !/^\d+$/u.test(rawRevision)
  ) {
    return;
  }
  void hydratePublicResume(root, slug[0]!, rawRevision);
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', boot, { once: true });
} else {
  boot();
}
