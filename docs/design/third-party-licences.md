# Third-party licences in release images

The owner approved this on 2026-11-15. aboutme publishes four images (server,
web, caddy, observer) and serves JavaScript bundles to every visitor. They carry
code under MIT, BSD, ISC, Apache-2.0, and similar licences that require the
copyright and licence text in every copy, binaries included, and base layers
that carry GPL software. This page owns how releases meet those terms.

## What each image carries

Every image holds `/licenses/`, generated at build time from the exact inputs
the image ships:

- `INDEX.txt`: one line per component with its kind (first-party, third-party,
  vendored asset, or base layer), name, version, SPDX identifier, and the paths
  of its licence files.
- One directory per third-party component with every `LICENSE*`, `LICENCE*`,
  `COPYING*`, and `NOTICE*` file it ships, copied byte for byte.
- The aboutme AGPL-3.0 licence as the first-party entry
  ([ADR 0001](../adr/0001-agpl-3.0-license.md)).
- `BASE-IMAGES.txt`, `SOURCE-OFFER.txt`, and base-layer notices, described
  below.

### Go images

The server image ships `server`, `render-browser-supervisor`, `migrate`, and
`db-setup`; the caddy image ships Caddy with its plugins; the observer image
ships `observer`. For each command the generator runs `go list -deps` with the
same `CGO_ENABLED`, `GOOS`, `GOARCH`, and build tags as the release build. It
resolves each package to its real source directory, following `replace`
directives, and collects:

- the licence files at the module root, and any licence file in a linked
  package's own directory or its parents inside the module, which covers code
  vendored inside a module;
- the Go licence and patents files of the exact toolchain version, for the
  linked runtime and standard library.

### Web image

The web image ships the Nitro server, the client bundles, and the separately
built render workers. For every bundler run that feeds the image, the generator
takes the packages of the modules in the emitted chunks from that bundler's own
output: each Vite or Rolldown build (client, SSR, and every render worker)
through Vite's `build.license` report or Rolldown's per-chunk module list, and
Nitro's final Rollup build through the chunk module lists its Rollup output
reports. It adds every package Nitro copies to `.output/server/node_modules`.
Licence files come from the installed packages in the pnpm store, read before
the image is assembled.

### Vendored assets

The icon marks and Unicode data recorded in `THIRD_PARTY_NOTICES.md`, and the
font notices, are copied into `/licenses/` with their creator, source, licence,
and modification notes, so the notices travel with the image.

## The public notice

The client bundles reach browsers, not only the image. The web build writes one
public file, `/licenses/THIRD_PARTY_NOTICES.txt`, with the client-side and
vendored entries and their licence texts, and a reference to the font notices.
It is served as plain text like the font notices.

`licenses` joins the public roots through `scripts/generate-public-roots.mjs`,
together with the existing public directories `font-licenses` and `landing`,
which are valid resume slugs today and not yet reserved. Before the release that
adds them, a read-only production query confirms that no resume holds any of the
three slugs. If one does, the release stops for an owner decision.

## Licence policy

The generator fails the build when a third-party component has no licence file
or a licence outside this list:

`MIT`, `MIT-0`, `ISC`, `0BSD`, `BSD-2-Clause`, `BSD-3-Clause`, `Apache-2.0`,
`BlueOak-1.0.0`, `CC0-1.0`, `CC-BY-4.0`, `OFL-1.1`, `Unicode-3.0`.

- An `OR` expression passes when one choice is on the list, and the index
  records that choice. `dompurify` therefore ships under Apache-2.0.
- MPL-2.0 alone is refused: shipping an MPL file needs a source location for
  that version, which this design does not provide.
- Apache-2.0 components ship their `NOTICE` file when they have one.
- CC-BY-4.0 entries carry the creator, source, licence, and any modification.

A component whose published package lacks its licence file is listed in
`third_party/licences/overrides.json` with its SPDX identifier, the upstream URL
of the file, and a committed copy under `third_party/licences/`. For example,
`github.com/altcha-org/altcha-lib-go/v2` keeps its MIT licence at the repository
root, outside the `v2` module. A reviewed change adds each override, and a
version change of an overridden component fails until the override is checked
again.

GPL, LGPL, AGPL, SSPL, BUSL, and non-commercial licences never enter as
third-party application code without an owner decision recorded here. The
first-party AGPL entry and the base layers below sit outside this list.

## Base layers and the corresponding source

The runtime images build on unmodified upstream images pulled by digest: the
official Node.js Alpine image for web, Alpine for caddy, Ubuntu for the server,
and none for the observer. Those layers include GPL and LGPL software, such as
BusyBox on Alpine and GNU packages on Ubuntu, some under GPLv3.

Images carry only what they run. The server's runtime stage moves from the
Playwright image, which also carries Firefox, WebKit, FFmpeg, a headless shell,
and Yarn, to Ubuntu noble with the system packages Chromium needs and the same
pinned Chromium build Playwright installs, so rendering does not change. That
move is its own change and lands before this one.

For each runtime image, the generator reads the package database of the final
filesystem (`apk` or `dpkg`) and writes `BASE-IMAGES.txt`: every base image by
digest, then every installed package with its exact version, SPDX licence, and
the URL of that version's source in the distribution's public archive (the
Ubuntu source package on Launchpad, the Alpine aports commit and source files).
This file sits in the image and beside it in the GitHub release, and it is the
clear direction to the corresponding source that GPLv3 section 6(d) allows on a
third-party server.

Every file in a runtime image must be accounted for, and the build fails on any
file that is not. A file is accounted for when it belongs to one of:

- an installed package in the image's `apk` or `dpkg` database;
- the image's own build output, covered by the sections above;
- an entry in `third_party/licences/components/<image>.json`, committed for each
  component installed outside a package manager. An entry names the component,
  its exact version, its paths, the sha256 of its content, its licence files or
  complete credits, and the URL of that version's source.

Each component entry also goes into the image's `BASE-IMAGES.txt` with its
version, notices, and source URL, so it reaches recipients in the image and the
GitHub release and falls under the weekly source check below.

The build compares each listed component's content hash with its entry, so a
newer CA bundle or Chromium build at an unchanged path fails until a reviewed
change updates the entry. The components known today are the Node.js runtime and
the image's entrypoint script under `/usr/local` in the web image, whose
versioned `LICENSE` covers Node's bundled notices; the Chromium build in the
server image, which keeps that build's complete third-party credits; and the CA
bundle the observer copies from Alpine's `ca-certificates`, whose apk version is
pinned in the observer build.

`SOURCE-OFFER.txt` adds a written offer, valid for three years from the image's
release, to provide the corresponding source of every GPL or LGPL component on
request to `danny@aboutme.vn` (owner, 2026-11-15). It covers GPLv2 components
such as BusyBox.

Source access must last as long as an image is downloadable. A weekly scheduled
job checks every source URL in the `BASE-IMAGES.txt` of every published release.
A missing URL opens an issue, and within seven days the owner either uploads
that source version to the affected releases as an asset and updates the
directions, or makes the affected images unavailable from the registry.

The owner accepts the residual risk that an archive drops a version before it is
mirrored, a gap of up to fourteen days (owner, 2026-11-15). Launchpad and the
upstream archives keep old versions, and a per-release mirror would add large
uploads to every release.

A base package whose installed files lack its required notice, such as musl,
whose package ships only its libraries, gets the notice from
`third_party/licences/base/`, committed per base image digest. The build fails
when the package list of a pinned base image no longer matches its committed
inventory, so a digest change brings a reviewed notice update.

## Checks

- The generator runs in each image build and in `release-check` on the exact
  commit. A failure names the component and the reason.
- `release-image-check` extracts each published image's filesystem on the
  runner, so the shell-free observer is checked too, and asserts that every path
  in `INDEX.txt` exists, the first-party entry, `BASE-IMAGES.txt`, and
  `SOURCE-OFFER.txt` are present, and every package in the web build's client
  chunk report appears in the public notice.
- Unit tests feed the generator fixtures for a missing licence file, a licence
  outside the list, an `OR` expression, MPL-2.0 alone, a `NOTICE` file, a
  `replace` directive, a licence file in a package subdirectory, and an override
  whose version no longer matches.

## Order

This change follows the pnpm workspace, which gives one lockfile and the store
the web generator reads, and the removal of the undeployed AWS code, which drops
modules the images no longer need.
