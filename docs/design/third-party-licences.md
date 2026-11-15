# Third-party licences in release images

The owner approved this on 2026-11-15. aboutme publishes four images (server,
web, caddy, observer) and serves JavaScript bundles to every visitor. Each
carries code under MIT, BSD, ISC, Apache-2.0, and similar licences that require
the copyright and licence text in every copy, binaries included. This page owns
how releases carry those texts. Vendored assets keep their notices in
`THIRD_PARTY_NOTICES.md` and the generated font notices.

## What each image carries

Every image holds `/licenses/`, generated at build time from the exact inputs
the image compiles:

- `INDEX.txt`: one line per component with name, version, SPDX identifier, and
  the path of its licence files.
- One directory per component with every `LICENSE*`, `LICENCE*`, `COPYING*`, and
  `NOTICE*` file it ships, copied byte for byte.

Components per image:

- **Go images** (server, caddy, observer): every module that `go list -deps`
  reports for the image's main packages under the release build tags and target
  platform, from the module cache that built the binary.
- **Web image**: every package in `.output/server/node_modules`, plus every
  package whose code the bundler placed in `.output/public/_nuxt`, taken from
  the bundler's module graph rather than the whole dependency tree.
- Base-image packages (Alpine, the Playwright image's Ubuntu and Chromium) keep
  the licence files their distributions install. This page adds none for them.

## The public notice

The JavaScript bundles reach browsers, not only the image. The web build writes
the client-bundle part of the index and licence texts to one public file,
`/licenses/THIRD_PARTY_NOTICES.txt`, served as plain text like the font notices.
`licenses` joins the public roots as a Nuxt root through
`scripts/generate-public-roots.mjs`, so no resume slug can claim it. The privacy
and terms pages need no change; the repository's `THIRD_PARTY_NOTICES.md` links
the file.

## Licence policy

The generator fails the build when a component's licence is not on this list, or
when it finds no licence file:

`MIT`, `MIT-0`, `ISC`, `0BSD`, `BSD-2-Clause`, `BSD-3-Clause`, `Apache-2.0`,
`BlueOak-1.0.0`, `MPL-2.0`, `CC0-1.0`, `CC-BY-4.0`, `OFL-1.1`, `Unicode-3.0`,
`Python-2.0`.

An `OR` expression passes when one choice is on the list, and the index records
that choice. Apache-2.0 components ship their `NOTICE` file when they have one.

A component whose published package lacks its licence file is listed in
`third_party/licences/overrides.json` with its SPDX identifier, the upstream URL
of the file, and a committed copy under `third_party/licences/`. For example,
`github.com/altcha-org/altcha-lib-go/v2` keeps its MIT licence at the repository
root, outside the `v2` module. A reviewed change adds each override. A version
change of an overridden component fails until the override is checked again.

No component under GPL, LGPL, AGPL, SSPL, BUSL, or a non-commercial licence
enters an image without an owner decision recorded here.

## Checks

- The generator runs in each image build and in `release-check` on the exact
  commit. A failure names the component and the reason.
- `release-image-check` asserts that `/licenses/INDEX.txt` exists in every
  published image and lists the image's own main module or package.
- A unit test feeds the generator fixtures for a missing licence file, a licence
  outside the list, an `OR` expression, a `NOTICE` file, and an override whose
  version no longer matches.

## Order

This change follows the pnpm workspace, which gives one lockfile and
`pnpm licenses list` for the package metadata, and the removal of the undeployed
AWS code, which drops modules the images no longer need.
