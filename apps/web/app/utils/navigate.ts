import {
  DEFAULT_RETURN_PATH,
  isAppRoute,
  validateReturnPath,
} from './returnPath';

/**
 * Leaves the current page for `path`, a same-origin path.
 *
 * A path this app's own router renders moves that router. Nuxt's `navigateTo`
 * declines to move it while another navigation is still settling: it returns
 * the path unchanged and does nothing, so the page stays where it is with
 * nothing to show, which is how a finished sign-in used to strand a visitor on
 * the login form. `router.push` supersedes the settling navigation instead, so
 * the destination always wins. Never put `navigateTo` back for an in-app path.
 *
 * Any other same-origin path is one the client router cannot render, for
 * example `/oauth/authorize`, served by the Go backend for a connected-agent
 * consent link. That needs a real browser navigation, which the in-flight rule
 * does not touch. Such a path is re-checked here (the same check `?next=` and
 * a stored return path get) before it becomes a top-level navigation, so no
 * caller can turn an unchecked string into one; an unusable path lands in the
 * app instead.
 */
export async function goToPath(path: string): Promise<void> {
  const router = useRouter();
  if (isAppRoute(path)) {
    await router.push(path);
    return;
  }
  const checked = validateReturnPath(path);
  if (checked === null) {
    await router.push(DEFAULT_RETURN_PATH);
    return;
  }
  await navigateTo(checked, { external: true });
}
