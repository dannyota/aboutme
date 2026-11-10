# Public browser evidence excludes test titles, errors, responses, and traces.
import json
from pathlib import Path
import re
import shutil
import stat
import struct
import sys
import tarfile
import tempfile
import zlib

def pixel_png(content):
    # Accept only the bounded, non-interlaced RGB/RGBA format emitted by the browser.
    try:
        if not content.startswith(b"\x89PNG\r\n\x1a\n") or len(content) > 32 * 1024 * 1024:
            return None
        offset, header, compressed, ended = 8, None, bytearray(), False
        idat_seen, idat_finished = False, False
        while offset + 12 <= len(content):
            size = struct.unpack_from(">I", content, offset)[0]
            kind = content[offset + 4:offset + 8]
            end = offset + 12 + size
            if end > len(content) or not re.fullmatch(b"[A-Za-z]{4}", kind):
                return None
            data = content[offset + 8:end - 4]
            if zlib.crc32(kind + data) != struct.unpack_from(">I", content, end - 4)[0]:
                return None
            if header is None and kind != b"IHDR":
                return None
            if kind == b"IHDR":
                if header is not None or size != 13:
                    return None
                width, height, depth, color, compression, filtering, interlace = struct.unpack(">IIBBBBB", data)
                if not (0 < width <= 16384 and 0 < height <= 16384 and width * height <= 16 * 1024 * 1024):
                    return None
                if depth != 8 or color not in [2, 6] or (compression, filtering, interlace) != (0, 0, 0):
                    return None
                header = data
            elif kind == b"IDAT":
                if idat_finished:
                    return None
                idat_seen = True
                compressed.extend(data)
            elif kind == b"IEND":
                if size != 0:
                    return None
                ended = idat_seen
                break
            elif kind == b"tRNS" or not (kind[0] & 32):
                return None
            if idat_seen and kind != b"IDAT":
                idat_finished = True
            offset = end
        if not ended or header is None:
            return None
        channels = 3 if color == 2 else 4
        stride = width * channels
        expected_size = (stride + 1) * height
        decoder = zlib.decompressobj()
        raw = decoder.decompress(compressed, expected_size + 1)
        if len(raw) != expected_size or not decoder.eof or decoder.unused_data or decoder.unconsumed_tail:
            return None
        pixels = bytearray()
        previous = bytearray(stride)
        for start in range(0, len(raw), stride + 1):
            filter_type = raw[start]
            if filter_type > 4:
                return None
            row = bytearray(raw[start + 1:start + 1 + stride])
            for index in range(stride) if filter_type else []:
                left = row[index - channels] if index >= channels else 0
                up = previous[index]
                upper_left = previous[index - channels] if index >= channels else 0
                if filter_type == 1:
                    predictor = left
                elif filter_type == 2:
                    predictor = up
                elif filter_type == 3:
                    predictor = (left + up) // 2
                elif filter_type == 4:
                    estimate = left + up - upper_left
                    distances = [abs(estimate - value) for value in [left, up, upper_left]]
                    predictor = [left, up, upper_left][distances.index(min(distances))]
                else:
                    predictor = 0
                row[index] = (row[index] + predictor) & 255
            pixels.append(0)
            pixels.extend(row)
            previous = row
        def chunk(kind, data):
            return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data))
        return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", header) + chunk(b"IDAT", zlib.compress(pixels)) + chunk(b"IEND", b"")
    except (ValueError, struct.error, zlib.error):
        return None

# IDs name synthetic fixture families. Result attachments select the case.
FIXTURES = {
    "screenshot.spec.ts": "renderer",
    "resume-screen.spec.ts": "resume-screen",
    "guide-screenshot.spec.ts": "guide",
    "chrome.spec.ts": "chrome",
    "gallery.spec.ts": "gallery-detail",
    "gallery-baselines.spec.ts": "library",
    "verify.spec.ts": "deployment",
    "card.spec.ts": "card",
    "sign-in-gate.spec.ts": "sign-in",
    "showcase.spec.ts": "showcase",
    "showcase-baselines.spec.ts": "showcase-display",
    "print.spec.ts": "overflow-pdf",
    "print-pdf.spec.ts": "print-pdf",
    "print-mutations.spec.ts": "print-mutations",
    "sample-pages.spec.ts": "sample-pages",
    "samples.spec.ts": "samples",
    "fonts-offline.spec.ts": "fonts",
    "corpus.spec.ts": "corpus",
    "normal-csp.spec.ts": "normal-csp",
    "preview-gap.spec.ts": "preview-gap",
}
REVIEWS = {
    "public-modern-sidebar-vi-dark-390": "resume-screen.spec.ts",
    "public-executive-band-en-light-1440": "resume-screen.spec.ts",
    "home-vi-light-390": "chrome.spec.ts",
    "showcase-en-dark-1440": "showcase-baselines.spec.ts",
    "library-sheet-en-light-390": "gallery-baselines.spec.ts",
    "sign-in-error-vi-390": "sign-in-gate.spec.ts",
    "print-main-overflow-p2": "print.spec.ts",
    "print-sidebar-overflow-p2": "print.spec.ts",
}


def regular(path):
    return (all(not parent.is_symlink() for parent in [path, *path.parents])
            and stat.S_ISREG(path.stat().st_mode))


def clear(path):
    if path.is_symlink() or (path.exists() and not path.is_dir()):
        path.unlink()
    elif path.exists():
        shutil.rmtree(path)


def attachment_png(attachment, surface):
    if attachment.get("contentType") != "image/png":
        return None
    value = attachment.get("path")
    if not isinstance(value, str):
        return None
    path = Path(value)
    if ".." in path.parts:
        return None
    roots = [Path("/results") / surface / "artifacts",
             mode.absolute() / surface / "artifacts"]
    for root in roots:
        if path.is_relative_to(root):
            local = mode / surface / "artifacts" / path.relative_to(root)
            try:
                if regular(local) and local.stat().st_size <= 32 * 1024 * 1024:
                    return pixel_png(local.read_bytes())
            except OSError:
                return None
    return None


mode, archive = map(Path, sys.argv[1:3])
commit, digest, manifest_hash, tar_hash = sys.argv[3:7]
status = int(sys.argv[7])
destination = mode / "diagnostics"
review_destination = mode / "review-images"
clear(destination)
clear(review_destination)
if not (re.fullmatch(r"[0-9a-f]{40}", commit)
        and re.fullmatch(r"sha256:[0-9a-f]{64}", digest)
        and all(re.fullmatch(r"[0-9a-f]{64}", value)
                for value in [manifest_hash, tar_hash])):
    raise ValueError("invalid source identity")
staged = Path(tempfile.mkdtemp(prefix="diagnostics-", dir=mode.parent))
review = Path(tempfile.mkdtemp(prefix="review-", dir=mode.parent))
try:
    with tarfile.open(archive) as tar:
        specs = {Path(member.name).name for member in tar.getmembers()
                 if member.isfile() and str(Path(member.name).parent)
                 == "apps/web/e2e" and Path(member.name).name in FIXTURES}
    summary = {"exit_status": status, "surfaces": {}}
    available = {}
    for surface in ["harness", "normal"]:
        facts = {"report": "unavailable", "failures": []}
        report = mode / surface / "results.json"
        if report.exists() and regular(report) and report.stat().st_size <= 16 * 1024 * 1024:
            try:
                data = json.loads(report.read_text())

                def cases(suites):
                    for suite in suites:
                        yield from suite.get("specs", [])
                        yield from cases(suite.get("suites", []))

                # Validate before publishing any images from this report.
                selected = []
                for spec, owner in ((spec, Path(suite.get("file", "")).name)
                                    for suite in data.get("suites", []) for spec in cases([suite])):
                    file = Path(spec.get("file", "")).name
                    if file not in specs:
                        file = owner
                    line = spec.get("line")
                    if file not in specs or type(line) is not int or not 0 < line < 100000:
                        continue
                    for test in spec.get("tests", []):
                        for result in test.get("results", []):
                            state = result.get("status")
                            attachments = result.get("attachments", [])
                            if not isinstance(attachments, list) or not all(isinstance(item, dict) for item in attachments):
                                raise ValueError("invalid attachments")
                            selected.append((file, line, state, attachments))
                for index, (file, line, state, attachments) in enumerate(selected):
                    if state in ["failed", "timedOut", "interrupted"]:
                        facts["failures"].append({"file": file, "line": line, "status": state})
                        for attachment in attachments:
                            if attachment.get("name") != "screenshot":
                                continue
                            content = attachment_png(attachment, surface)
                            if content is not None:
                                image_directory = staged / surface / str(index)
                                image_directory.mkdir(parents=True, exist_ok=True)
                                (image_directory / f"{FIXTURES[file]}.png").write_bytes(content)
                                break
                    if status == 0 and state == "passed":
                        for fixture, owner in REVIEWS.items():
                            if owner != file:
                                continue
                            available.setdefault(fixture, "unavailable")
                            for attachment in attachments:
                                name = attachment.get("name")
                                if name not in [f"review-{fixture}", f"review-{fixture}-unavailable"]:
                                    continue
                                available.setdefault(fixture, "unavailable")
                                if name == f"review-{fixture}":
                                    content = attachment_png(attachment, surface)
                                    if content is not None:
                                        (review / f"{fixture}.png").write_bytes(content)
                                        available[fixture] = "available"
                facts["report"] = "available"
            except (ValueError, TypeError, AttributeError, RecursionError):
                facts["failures"] = []
        summary["surfaces"][surface] = facts
    metadata = (f"commit={commit}\nmanifest_sha256={manifest_hash}\n"
                f"tar_sha256={tar_hash}\nbrowser_image_digest={digest}\n")
    (staged / "failure-summary.json").write_text(json.dumps(summary, sort_keys=True) + "\n")
    (staged / "source-metadata.txt").write_text(metadata)
    staged.rename(destination)
    if status == 0:
        (review / "source-metadata.txt").write_text(metadata)
        (review / "capture-summary.json").write_text(json.dumps(available, sort_keys=True) + "\n")
        review.rename(review_destination)
finally:
    for directory in [staged, review]:
        if directory.exists():
            shutil.rmtree(directory)
