# Task 7.1.6: Chromium controller

Render one private snapshot with pinned Chromium and return bounded bytes only
after every browser process has exited.

## Authorities and ownership

Read ADR 0023, ADR 0032, `docs/design/templates/print.md`, the print contract,
and task 7.1's queue interface. Acceptance: AC-PDF-001 through AC-PDF-004 and
AC-PDF-006. The browser feasibility probe is evidence, not a new authority.

One implementation author owns `apps/server/internal/printrender/` only. Root
owns dependency pins, configuration, composition, deployment, browser harnesses,
measurements, and documentation. The design/probe author does not implement this
slice.

## Interface

`New(Config) (*Renderer, error)` validates the executable and direct Nuxt
origin. `Render(context.Context, renderjob.Navigation) ([]byte, error)`
satisfies the queue's existing renderer interface. `Ready() error` reports
startup/browser availability; it must not create a browser on every readiness
request.

Config carries the executable path, a validated `directrender.RenderOrigin`, and
an optional test seam if needed. Pin expected browser version to
`151.0.7922.34`. Reject root execution and unsupported sandbox hosts. Do not
accept a caller-provided URL, flags, script, capture geometry, or request
headers. Use chromedp v0.15.1, with root adding the manifest dependency.

## Lifecycle and capture

- Create a fresh browser/profile per attempt. Keep the sandbox enabled
  explicitly with `no-sandbox=false`. Do not import chromedp's default flags
  wholesale. Do not disable site isolation or enable unsafe SwiftShader.
- Pin UTC, C.UTF-8 locale, sRGB, font hinting none, disabled LCD text, GPU off,
  hidden scrollbars, and device scale 1. Disable background networking,
  extensions, sync, component updates, default apps, first-run actions, metrics,
  crash uploads, speculative/preconnect networking, and browser automation
  features that initiate unrelated network traffic.
- Launch Chromium and its version probe with only `TZ=UTC`, `LANG=C.UTF-8`, and
  `LC_ALL=C.UTF-8`. Clear the launcher environment too. A synthetic parent
  secret must not reach the executed child; test failures never print raw
  environment values.
- Set a new process group and parent-death SIGKILL. Replace command cancellation
  with SIGKILL of the negative process-group ID. Always cancel and wait for the
  allocator and process group before returning, including successful capture.
  Cancellation during startup, navigation, readiness, or output read follows the
  same joined cleanup. No detached goroutine may retain job authority.
- The queue supplies the 20-second admission deadline. Never replace it with a
  longer context. Avoid logging commands, navigation URLs, headers, browser
  output, resume IDs, or raw dependency errors.
- Navigate only to exact direct-origin `/print/<resumeId>`. Confirm HTTP 200,
  script-free print CSP, and the expected `data-print-document=true` root. Await
  fonts and decode every inline image; reject failed fonts/images.
- PDF uses print media, CSS page size, background printing, no browser headers
  or footers, scale 1, and zero job margins. Read PDF through a bounded CDP
  stream; stop once 16,777,216 bytes would be exceeded.
- PNG uses screen media and exactly 1200 by 630 pixels at scale 1, an opaque
  white page background, top viewport only, and a 4,194,304-byte output bound.
  Validate output signature and dimensions. There are no alternate formats.
- The controller returns bytes only; the queue recomputes the digest and owns
  private completion and public-generation validation.

## Page network authority

Start a deny-by-default HTTP proxy on an ephemeral loopback port for each
attempt. Give Chromium `proxy-server` and `proxy-bypass-list=<-loopback>` so
even loopback requests pass it. Reject CONNECT and every method or absolute URL
outside the one initial document GET and exact CSS/font allowlist. Forward only
to the fixed configured Nuxt address with no redirects or environment proxy.
Close and join the proxy and all forwarding requests before returning. This
covers background requests outside the page's Fetch session.

Install Fetch interception for every request before navigation. Admit exactly
one initial main-frame GET for the exact print URL. Strip ambient Cookie,
Authorization, and X-Render-Job-ID headers, then add the supplied capability and
job ID only to that request. Never use browser-wide extra authority headers.

All other requests are denied except these exact same-origin asset paths:
`/_nuxt/assets/print.css`, `/_nuxt/assets/print-fonts.css`, and the repository's
pinned WOFF2 filenames. Build the font path allowlist from the fixed catalog,
not a broad extension or path-prefix rule. Assets carry no authority or cookies.
Deny redirected documents, repeated document requests, non-GETs, external URLs,
service workers, frames, popups, workers, WebSockets, and unlisted same-origin
paths. Require the private route's closed CSP, including sandbox
allow-same-origin, fresh profile, and no scripts. Install browser auto-attach
with wait-for-debugger before navigation; any unowned
popup/worker/service-worker target cancels the attempt before it can run. Cancel
the job on an unexpected target or an interception failure. Join request
interception callbacks before returning.

These controls restrict page-originated traffic. They are not a claim of kernel
egress isolation against a compromised Chromium process. Deployment network
policy remains an additional boundary; do not weaken sandbox or task networking
to make local tests pass.

## Verification and report

Write failing tests first. Include exact URL/header allowlists, duplicate and
redirect admission, format/output bounds, unknown format, canceled contexts,
entropy-free controller behavior, opaque failures, font/image readiness, and
joined process cancellation. Add an opt-in real pinned-browser test using a
local stub private route with fixed headers/assets and malicious attempts;
record that it was run. Kernel-assigned test ports are exempt, but production
origins remain restricted to the approved fixed listeners.

```sh
flock --close .dev/phase-7/heavy.lock sh -c \
  'cd apps/server && go test -race -count=1 ./internal/printrender'
```

Report changed paths, failing-first evidence, exact test outcomes, lifecycle and
network evidence, and required root integration. Do not use Git, modify
manifests, read secrets, run full CI, or change containers.
