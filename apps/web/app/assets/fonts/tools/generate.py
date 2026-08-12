#!/usr/bin/env python3
"""Task 5 font vendoring pipeline.

Reads the frozen 26-row input matrix from docs/design/fonts.md, downloads
only the exact pinned official inputs, enforces the per-asset license gate,
subsets where the declared policy allows, and regenerates every derived
artifact:

  apps/web/app/assets/fonts/*.woff2       final self-hosted assets
  apps/web/app/assets/fonts/catalog.json  the committed manifest
  apps/web/app/assets/css/fonts.css       local-only @font-face rules
  apps/web/public/font-licenses/**        per-family license copies
  apps/web/public/font-licenses/THIRD_PARTY_NOTICES.txt

Every download is verified against the design's SHA-256 before use. A row
that fails its hash or license check is EXCLUDED and recorded in
catalog.json's "excluded" list with the reason; the pipeline never
substitutes a different source or version. apps/web/test/fonts.test.ts
asserts the excluded list is empty, so an exclusion fails the suite loudly
instead of shipping silently.

Usage (see ../README.md for the pinned environment):
  python3 generate.py --cache <download-cache-dir>
"""

from __future__ import annotations

import argparse
import hashlib
import io
import json
import platform
import re
import subprocess
import sys
import urllib.parse
import urllib.request
import zipfile
from dataclasses import dataclass, field
from importlib import metadata
from pathlib import Path

from fontTools.subset import Options, Subsetter, load_font, parse_unicodes, \
    save_font
from fontTools.ttLib import TTFont

TOOLS_DIR = Path(__file__).resolve().parent
FONTS_DIR = TOOLS_DIR.parent
WEB_ROOT = FONTS_DIR.parents[2]
REPO_ROOT = FONTS_DIR.parents[4]
DESIGN_PATH = REPO_ROOT / 'docs' / 'design' / 'fonts.md'
CSS_PATH = FONTS_DIR.parent / 'css' / 'fonts.css'
LICENSES_DIR = WEB_ROOT / 'public' / 'font-licenses'
FIXTURE_PATH = WEB_ROOT / 'test' / 'fixtures' / 'font-coverage.txt'

# The final web subset: ASCII, Latin-1, Latin Extended-A, the Vietnamese
# horn letters, spacing modifier accents, combining diacritics, Latin
# Extended Additional (Vietnamese precomposed), general punctuation, the
# dong and euro signs, the trademark sign, and the minus sign. This must
# remain a superset of the committed coverage fixture.
SUBSET_UNICODES = ('0020-007E,00A0-017F,01A0-01B0,02C6-02DD,0300-036F,'
                   '1E00-1EFF,2000-206F,20AB-20AC,2122,2212')

FALLBACK_BY_CATEGORY = {
    'sans': 'noto-sans',
    'serif': 'noto-serif',
    'slab serif': 'noto-serif',
    'display serif': 'noto-serif',
    'monospace': 'space-mono',
}

CMAP_PREFERENCE = [(3, 10), (0, 6), (0, 4), (3, 1), (0, 3), (0, 2),
                   (0, 1), (0, 0)]

REQUIRED_LICENSE_PHRASES = [
    # The OFL-1.1 grant that makes every required right fee-free.
    'Permission is hereby granted, free of charge',
    'to use, study, copy, merge, embed, modify, redistribute, and sell',
    # Documents (PDF/image embedding) do not inherit the license.
    'does not apply to any document created using the Font Software',
]

PINNED_PYTHON = '3.14.7'
PINNED_FONTTOOLS = '4.63.0'
PINNED_BROTLI = '1.2.0'


class GateError(Exception):
    """A row failed its provenance or license gate."""


def validate_toolchain() -> None:
    """Fail before output generation when the pinned toolchain drifts."""
    actual = {
        'python': platform.python_version(),
        'fonttools': metadata.version('fonttools'),
        'brotli': metadata.version('brotli'),
    }
    expected = {
        'python': PINNED_PYTHON,
        'fonttools': PINNED_FONTTOOLS,
        'brotli': PINNED_BROTLI,
    }
    drift = [
        f'{name}={actual[name]} (want {version})'
        for name, version in expected.items()
        if actual[name] != version
    ]
    if drift:
        raise GateError('unpinned generator toolchain: ' + ', '.join(drift))


@dataclass
class DesignRow:
    rank: int
    id: str
    display_name: str
    category: str
    repository: str
    commits: list[str]
    archive_ref: str | None
    inputs: list[tuple[str, str]]  # (path, sha256)
    license_path: str
    license_sha256: str
    rfns: list[str]
    policy: str
    v1_family: str


@dataclass
class Archive:
    ref: str
    url: str
    sha256: str


@dataclass
class BuiltAsset:
    filename: str
    role: str
    input_path: str
    data: bytes = field(repr=False, default=b'')
    weight: tuple = (400, 400)
    axes: list[dict] = field(default_factory=list)
    names: dict = field(default_factory=dict)
    codepoints: set[int] = field(default_factory=set)


def sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def parse_design(text: str) -> tuple[list[DesignRow], dict[str, Archive]]:
    rows: list[DesignRow] = []
    archives: dict[str, Archive] = {}
    pair_re = re.compile(r'`([^`]+)`\s+—\s+`([0-9a-f]{64})`')
    for line in text.split('\n'):
        m = re.match(
            r'^\|\s*(A\d+)\s*\|\s*`([^`]+)`\s*\|\s*`([0-9a-f]{64})`\s*\|',
            line)
        if m:
            archives[m.group(1)] = Archive(m.group(1), m.group(2),
                                           m.group(3))
            continue
        if not re.match(r'^\|\s*\d+\s*\|', line):
            continue
        cells = [c.strip() for c in line.split('|')]
        id_m = re.match(
            r'^`([a-z0-9-]+)`\s+—\s+(.+);\s*'
            r'(sans|serif|slab serif|display serif|monospace)$', cells[2])
        repo_ms = re.findall(
            r'github\.com/([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)/tree/'
            r'([0-9a-f]{40})', cells[3])
        arch_m = re.search(r'Archive (A\d+),', cells[4])
        lic = pair_re.findall(cells[5])
        rfn_m = re.match(
            r'^(.+?)\s+/\s+(unmodified-upstream|subset-original-name|'
            r'subset-renamed)$', cells[6])
        if not id_m or not repo_ms or len(lic) != 1 or not rfn_m:
            raise GateError(f'unparseable design row: {line!r}')
        repos = {r for r, _ in repo_ms}
        if len(repos) != 1:
            raise GateError(f'multiple repositories in row: {line!r}')
        rows.append(DesignRow(
            rank=int(cells[1]),
            id=id_m.group(1),
            display_name=id_m.group(2).strip(),
            category=id_m.group(3),
            repository=f'https://github.com/{repos.pop()}',
            commits=[c for _, c in repo_ms],
            archive_ref=arch_m.group(1) if arch_m else None,
            inputs=pair_re.findall(cells[4]),
            license_path=lic[0][0],
            license_sha256=lic[0][1],
            rfns=[] if rfn_m.group(1) == 'None'
            else re.split(r',\s*', rfn_m.group(1)),
            policy=rfn_m.group(2),
            v1_family=cells[7],
        ))
    return rows, archives


def http_get(url: str, cache: Path) -> bytes:
    key = sha256(url.encode())[:16] + '-' + Path(
        urllib.parse.urlparse(url).path).name
    cached = cache / key
    if cached.exists():
        return cached.read_bytes()
    req = urllib.request.Request(
        url, headers={'User-Agent': 'aboutme-font-vendoring'})
    with urllib.request.urlopen(req, timeout=300) as resp:
        data = resp.read()
    cached.write_bytes(data)
    return data


def fetch_verified(url: str, expected: str, cache: Path,
                   label: str) -> bytes:
    data = http_get(url, cache)
    actual = sha256(data)
    if actual != expected:
        raise GateError(
            f'{label}: SHA-256 mismatch for {url}: '
            f'expected {expected}, got {actual}')
    return data


def raw_url(repository: str, commit: str, path: str) -> str:
    owner_repo = repository.removeprefix('https://github.com/')
    quoted = urllib.parse.quote(path, safe='/')
    return (f'https://raw.githubusercontent.com/{owner_repo}/'
            f'{commit}/{quoted}')


def zip_member(archive_data: bytes, inner_path: str, label: str) -> bytes:
    with zipfile.ZipFile(io.BytesIO(archive_data)) as zf:
        names = zf.namelist()
        if inner_path in names:
            return zf.read(inner_path)
        suffix = [n for n in names if n.endswith('/' + inner_path)]
        if len(suffix) == 1:
            return zf.read(suffix[0])
        raise GateError(
            f'{label}: {inner_path!r} not found uniquely in archive '
            f'({len(suffix)} suffix matches)')


def fetch_row_inputs(row: DesignRow, archives: dict[str, Archive],
                     cache: Path) -> dict[str, bytes]:
    """Returns input path -> verified bytes, or raises GateError."""
    out: dict[str, bytes] = {}
    if row.archive_ref is not None:
        archive = archives[row.archive_ref]
        data = fetch_verified(archive.url, archive.sha256, cache,
                              f'{row.id} archive {archive.ref}')
        for path, digest in row.inputs:
            member = zip_member(data, path, row.id)
            if sha256(member) != digest:
                raise GateError(
                    f'{row.id}: inner file {path} hash mismatch')
            out[path] = member
        return out
    for path, digest in row.inputs:
        out[path] = fetch_verified(
            raw_url(row.repository, row.commits[0], path), digest, cache,
            f'{row.id} input {path}')
    return out


def fetch_license(row: DesignRow, cache: Path) -> bytes:
    last_error: GateError | None = None
    # roboto-serif pins a source commit and a binary-release commit; the
    # license must hash-match the design from one of the pinned commits.
    for commit in row.commits:
        try:
            return fetch_verified(
                raw_url(row.repository, commit, row.license_path),
                row.license_sha256, cache, f'{row.id} license')
        except GateError as err:
            last_error = err
    assert last_error is not None
    raise last_error


def embedded_rfn_declarations(data: bytes) -> list[str]:
    """RFN declarations embedded in a font's own notices.

    Looks at the copyright (0) and license-description (13) name records
    for the OFL declaration formula "with Reserved Font Name ...". The
    bare phrase is not enough for record 13, which may quote generic OFL
    body text.
    """
    font = TTFont(io.BytesIO(data), fontNumber=0, lazy=True)
    found = []
    for record in font['name'].names:
        if record.nameID not in (0, 13):
            continue
        value = record.toUnicode()
        if re.search(r'with\s+Reserved\s+Font\s+Names?', value,
                     re.IGNORECASE):
            found.append(value)
    font.close()
    return found


def license_gate(row: DesignRow, text: str,
                 embedded: list[str]) -> dict:
    """Verifies the exact license text permits the declared policy.

    The design's RFN column covers declarations in the license file's
    preamble AND in the selected asset's embedded notices (`embedded`).
    Returns {'copyright': str, 'rfns': [str]} or raises GateError.
    """
    normalized = re.sub(r'\s+', ' ', text)
    if 'SIL OPEN FONT LICENSE Version 1.1' not in normalized:
        raise GateError(f'{row.id}: license is not OFL-1.1')
    for phrase in REQUIRED_LICENSE_PHRASES:
        if phrase not in normalized:
            raise GateError(
                f'{row.id}: OFL grant phrase missing: {phrase!r}')
    # The preamble ends where the OFL text begins: either the standard
    # "This Font Software is licensed ..." sentence or, in copies that
    # omit it (e.g. Open Sans), the OFL heading itself.
    markers = [m.start() for m in (
        re.search(r'This Font Software is licensed', text, re.IGNORECASE),
        re.search(r'SIL OPEN FONT LICENSE Version 1\.1', text),
    ) if m is not None]
    if not markers:
        raise GateError(f'{row.id}: no license preamble found')
    preamble = text[:min(markers)]
    declares_rfn = (re.search(r'Reserved Font Name', preamble,
                              re.IGNORECASE) is not None
                    or bool(embedded))
    if declares_rfn != bool(row.rfns):
        raise GateError(
            f'{row.id}: Reserved Font Name declaration does not match '
            f'the design (declared={declares_rfn}, design={row.rfns})')
    for rfn in row.rfns:
        in_preamble = re.search(
            r'Reserved Font Names?[^.]*' + re.escape(rfn),
            preamble, re.IGNORECASE | re.DOTALL) is not None
        in_embedded = any(rfn in value for value in embedded)
        if not in_preamble and not in_embedded:
            raise GateError(
                f'{row.id}: RFN {rfn!r} declared nowhere in the license '
                'preamble or the asset notices')
    if row.policy == 'subset-original-name' and declares_rfn:
        raise GateError(
            f'{row.id}: subsetting under the original name requires a '
            'license and asset with no Reserved Font Name')
    copyright_lines = [
        re.sub(r'\s+', ' ', line).strip()
        for line in preamble.split('\n')
        if line.strip() and not line.strip().startswith('#')
        and not re.fullmatch(r'-+', line.strip())
    ]
    if not copyright_lines or 'copyright' not in copyright_lines[0].lower():
        raise GateError(f'{row.id}: no copyright statement in preamble')
    return {'copyright': ' '.join(copyright_lines), 'rfns': list(row.rfns)}


def subset_to_woff2(data: bytes, unicodes: list[int]) -> bytes:
    options = Options()
    options.flavor = 'woff2'
    options.layout_features = ['*']
    options.name_IDs = ['*']
    options.name_languages = ['*']
    options.notdef_outline = True
    options.recalc_timestamp = False
    font = load_font(io.BytesIO(data), options)
    subsetter = Subsetter(options=options)
    subsetter.populate(unicodes=unicodes)
    subsetter.subset(font)
    buffer = io.BytesIO()
    save_font(font, buffer, options)
    font.close()
    return buffer.getvalue()


def integral(value):
    return int(value) if float(value) == int(float(value)) else float(value)


def best_name(font: TTFont, name_id: int) -> str:
    name = font['name']
    for args in ((name_id, 3, 1, 0x409), (name_id, 1, 0, 0)):
        record = name.getName(*args)
        if record is not None:
            return record.toUnicode()
    for record in name.names:
        if record.nameID == name_id:
            return record.toUnicode()
    return ''


def measure(data: bytes) -> dict:
    font = TTFont(io.BytesIO(data), fontNumber=0)
    names = {
        'family': best_name(font, 1),
        'subfamily': best_name(font, 2),
        'fullName': best_name(font, 4),
        'postScriptName': best_name(font, 6),
    }
    axes = []
    if 'fvar' in font:
        axes = [{'tag': a.axisTag, 'min': integral(a.minValue),
                 'default': integral(a.defaultValue),
                 'max': integral(a.maxValue)}
                for a in font['fvar'].axes]
    weight_class = font['OS/2'].usWeightClass
    subtable = None
    for platform_id, encoding_id in CMAP_PREFERENCE:
        subtable = font['cmap'].getcmap(platform_id, encoding_id)
        if subtable is not None:
            break
    if subtable is None:
        raise GateError('font has no usable cmap subtable')
    codepoints = {cp for cp, glyph_name in subtable.cmap.items()
                  if glyph_name != '.notdef' and cp != 0xFFFF}
    font.close()
    return {'names': names, 'axes': axes, 'weightClass': weight_class,
            'codepoints': codepoints}


def to_ranges(codepoints: set[int]) -> str:
    parts: list[str] = []
    ordered = sorted(codepoints)
    start = prev = None
    for cp in ordered:
        if start is None:
            start = prev = cp
            continue
        if cp == prev + 1:
            prev = cp
            continue
        parts.append(f'{start:X}' if start == prev
                     else f'{start:X}-{prev:X}')
        start = prev = cp
    if start is not None:
        parts.append(f'{start:X}' if start == prev
                     else f'{start:X}-{prev:X}')
    return ','.join(parts)


def build_row(row: DesignRow, archives: dict[str, Archive], cache: Path,
              unicodes: list[int], fixture: set[int]) -> dict:
    inputs = fetch_row_inputs(row, archives, cache)
    license_bytes = fetch_license(row, cache)
    embedded = [decl for data in inputs.values()
                for decl in embedded_rfn_declarations(data)]
    gate = license_gate(row, license_bytes.decode('utf-8'), embedded)

    assets: list[BuiltAsset] = []
    for path, digest in row.inputs:
        data = inputs[path]
        if row.policy == 'unmodified-upstream':
            final = data
        elif row.policy == 'subset-original-name':
            final = subset_to_woff2(data, unicodes)
        else:
            raise GateError(
                f'{row.id}: policy {row.policy} needs a reviewed renamed '
                'derivative; not implemented')
        if len(row.inputs) == 1:
            role, suffix = 'variable', 'var'
            if 'Regular' in path or 'Bold' in path:
                raise GateError(f'{row.id}: single static input?')
        elif 'Regular' in path:
            role, suffix = 'upright-400', '400'
        elif 'Bold' in path:
            role, suffix = 'upright-700', '700'
        else:
            raise GateError(f'{row.id}: cannot infer role for {path}')
        measured = measure(final)
        if role == 'variable':
            wght = next((a for a in measured['axes'] if a['tag'] == 'wght'),
                        None)
            if wght is None or wght['min'] > 400 or wght['max'] < 700:
                raise GateError(
                    f'{row.id}: variable input does not cover 400-700')
            weight = (wght['min'], wght['max'])
        else:
            expected = 400 if role == 'upright-400' else 700
            if measured['weightClass'] != expected:
                raise GateError(
                    f'{row.id}: {path} usWeightClass '
                    f'{measured["weightClass"]} != {expected}')
            weight = (expected, expected)
        assets.append(BuiltAsset(
            filename=f'{row.id}-{suffix}.woff2', role=role,
            input_path=path, data=final, weight=weight,
            axes=measured['axes'], names=measured['names'],
            codepoints=measured['codepoints']))

    assets.sort(key=lambda a: a.role)  # upright-400 < upright-700 < var
    covered = set.intersection(*(a.codepoints for a in assets))
    missing = sorted(cp for cp in fixture if cp not in covered)
    archive = archives[row.archive_ref] if row.archive_ref else None
    return {
        'row': row,
        'gate': gate,
        'license_bytes': license_bytes,
        'assets': assets,
        'entry': {
            'id': row.id,
            'displayName': row.display_name,
            'category': row.category,
            'cssFamily': row.display_name,
            'policy': row.policy,
            'spdx': 'OFL-1.1',
            'copyright': gate['copyright'],
            'reservedFontNames': gate['rfns'],
            'source': {
                'repository': row.repository,
                'commit': row.commits[0],
                'binaryCommit': row.commits[1] if len(row.commits) > 1
                else None,
                'archive': ({'ref': archive.ref, 'url': archive.url,
                             'sha256': archive.sha256}
                            if archive else None),
                'inputs': [{'path': p, 'sha256': d}
                           for p, d in row.inputs],
                'license': {'path': row.license_path,
                            'sha256': row.license_sha256},
            },
            'licenseFile': {
                'runtimePath': (f'font-licenses/{row.id}/'
                                + Path(row.license_path).name),
                'sha256': sha256(license_bytes),
            },
            'assets': [{
                'path': a.filename,
                'role': a.role,
                'input': a.input_path,
                'sha256': sha256(a.data),
                'bytes': len(a.data),
                'weight': [integral(a.weight[0]), integral(a.weight[1])],
                'axes': a.axes,
                'internalNames': a.names,
                'coverage': {'codepoints': len(a.codepoints),
                             'ranges': to_ranges(a.codepoints)},
            } for a in assets],
            'fixtureCoverage': {
                'complete': not missing,
                'missing': [f'U+{cp:04X}' for cp in missing],
            },
            'fallback': {'id': FALLBACK_BY_CATEGORY[row.category]},
            'v1Family': row.v1_family,
        },
    }


def emit_css(entries: list[dict]) -> str:
    lines = [
        '/* Generated by app/assets/fonts/tools/generate.py (Task 5). */',
        '/* Do not edit by hand; every source is a local vendored file. */',
        '',
    ]
    for entry in entries:
        lines.append(f'/* {entry["id"]} — {entry["displayName"]} */')
        for asset in entry['assets']:
            low, high = asset['weight']
            weight = f'{low}' if low == high else f'{low} {high}'
            lines.extend([
                '@font-face {',
                f"  font-family: '{entry['cssFamily']}';",
                '  font-style: normal;',
                f'  font-weight: {weight};',
                '  font-display: swap;',
                f"  src: url('../fonts/{asset['path']}') format('woff2');",
                '}',
                '',
            ])
    return '\n'.join(lines)


def emit_notices(entries: list[dict]) -> str:
    lines = [
        'Third-party font notices for the aboutme web application.',
        'Generated by app/assets/fonts/tools/generate.py. Do not edit.',
        '',
    ]
    for entry in entries:
        rfns = ', '.join(entry['reservedFontNames']) or 'none'
        lines.extend([
            f'{entry["id"]} — {entry["displayName"]}',
            f'  Copyright: {entry["copyright"]}',
            f'  SPDX license identifier: {entry["spdx"]}',
            f'  Reserved Font Names: {rfns}',
            f'  License file: {entry["licenseFile"]["runtimePath"]}',
            '',
        ])
    return '\n'.join(lines)


def design_revision() -> str:
    return subprocess.run(
        ['git', '-C', str(REPO_ROOT), 'log', '-1', '--format=%H', '--',
         'docs/design/fonts.md'],
        check=True, capture_output=True, text=True).stdout.strip()


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--cache', type=Path, required=True,
                        help='download cache directory')
    args = parser.parse_args()
    try:
        validate_toolchain()
    except GateError as err:
        print(f'FATAL: {err}', file=sys.stderr)
        return 1
    args.cache.mkdir(parents=True, exist_ok=True)

    design_text = DESIGN_PATH.read_text()
    rows, archives = parse_design(design_text)
    if len(rows) != 26:
        print(f'FATAL: expected 26 design rows, parsed {len(rows)}',
              file=sys.stderr)
        return 1

    fixture_bytes = FIXTURE_PATH.read_bytes()
    fixture = {ord(ch) for ch in fixture_bytes.decode('utf-8')
               if ch not in '\r\n'}
    unicodes = parse_unicodes(SUBSET_UNICODES)
    not_subsettable = fixture - set(unicodes)
    if not_subsettable:
        print(f'FATAL: fixture codepoints outside SUBSET_UNICODES: '
              f'{sorted(not_subsettable)}', file=sys.stderr)
        return 1

    built: list[dict] = []
    excluded: list[dict] = []
    for row in rows:
        try:
            built.append(build_row(row, archives, args.cache, unicodes,
                                   fixture))
            print(f'admitted: {row.id}')
        except GateError as err:
            excluded.append({'id': row.id, 'reason': str(err)})
            print(f'EXCLUDED: {row.id}: {err}', file=sys.stderr)

    entries = [b['entry'] for b in built]
    by_id = {e['id']: e for e in entries}
    for entry in entries:
        fallback = by_id.get(entry['fallback']['id'])
        if fallback is None:
            print(f'FATAL: fallback {entry["fallback"]["id"]} for '
                  f'{entry["id"]} was excluded', file=sys.stderr)
            return 1
        entry['fallback']['cssFamily'] = fallback['cssFamily']

    # The deterministic-fallback invariant: the committed fixture must be
    # renderable from the selected family plus its bundled fallback.
    coverage_by_id = {
        b['entry']['id']: set.intersection(
            *(a.codepoints for a in b['assets']))
        for b in built
    }
    for entry in entries:
        own = coverage_by_id[entry['id']]
        fb = coverage_by_id[entry['fallback']['id']]
        unreachable = sorted(cp for cp in fixture
                             if cp not in own and cp not in fb)
        if unreachable:
            print(f'FATAL: {entry["id"]}: fixture codepoints reach a '
                  f'platform font: {[f"U+{c:04X}" for c in unreachable]}',
                  file=sys.stderr)
            return 1

    # Write assets and remove stale ones.
    keep = {a.filename for b in built for a in b['assets']}
    for stale in FONTS_DIR.glob('*.woff2'):
        if stale.name not in keep:
            stale.unlink()
    for b in built:
        for asset in b['assets']:
            (FONTS_DIR / asset.filename).write_bytes(asset.data)

    # Rebuild the runtime license tree from scratch.
    if LICENSES_DIR.exists():
        for path in sorted(LICENSES_DIR.rglob('*'), reverse=True):
            path.unlink() if path.is_file() else path.rmdir()
    for b in built:
        target = WEB_ROOT / 'public' / b['entry']['licenseFile'][
            'runtimePath']
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(b['license_bytes'])
    (LICENSES_DIR / 'THIRD_PARTY_NOTICES.txt').write_text(
        emit_notices(entries))

    CSS_PATH.parent.mkdir(parents=True, exist_ok=True)
    CSS_PATH.write_text(emit_css(entries))

    catalog = {
        'catalogVersion': 2,
        'design': {
            'document': 'docs/design/fonts.md',
            'revision': design_revision(),
            'sha256': sha256(design_text.encode()),
        },
        'generator': {
            'tool': 'apps/web/app/assets/fonts/tools/generate.py',
            'python': PINNED_PYTHON,
            'fonttools': PINNED_FONTTOOLS,
            'brotli': PINNED_BROTLI,
            'subsetUnicodes': SUBSET_UNICODES,
        },
        'fixture': {
            'path': 'apps/web/test/fixtures/font-coverage.txt',
            'sha256': sha256(fixture_bytes),
        },
        'excluded': excluded,
        'entries': entries,
    }
    (FONTS_DIR / 'catalog.json').write_text(
        json.dumps(catalog, indent=2, ensure_ascii=False) + '\n')

    total = sum(a['bytes'] for e in entries for a in e['assets'])
    print(f'\nadmitted {len(entries)}/26, excluded {len(excluded)}')
    print(f'total vendored font bytes: {total}')
    for entry in entries:
        own = sum(a['bytes'] for a in entry['assets'])
        fb = by_id[entry['fallback']['id']]
        fb_bytes = 0 if fb['id'] == entry['id'] \
            else sum(a['bytes'] for a in fb['assets'])
        print(f'  {entry["id"]}: {own} + fallback {fb_bytes} '
              f'= {own + fb_bytes}')
    return 0 if not excluded else 2


if __name__ == '__main__':
    sys.exit(main())
