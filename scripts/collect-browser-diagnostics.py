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

mode, archive = map(Path, sys.argv[1:3])
commit, digest, manifest_hash, tar_hash = sys.argv[3:7]
status = int(sys.argv[7])
destination = mode / "diagnostics"
if destination.is_symlink() or (destination.exists() and not destination.is_dir()):
    destination.unlink()
elif destination.exists():
    shutil.rmtree(destination)
staged = Path(tempfile.mkdtemp(prefix="diagnostics-", dir=mode.parent))
try:
    with tarfile.open(archive) as tar:
        baselines = {}
        specs = set()
        for member in tar.getmembers():
            path = Path(member.name)
            if not member.isfile():
                continue
            if str(path.parent) in ["apps/web/e2e/baselines", "apps/web/e2e/print-baselines", "apps/web/public/templates/pages"] and re.fullmatch(r"[a-z0-9_-]+\.png", path.name):
                if member.size <= 32 * 1024 * 1024:
                    baselines[path.stem] = tar.extractfile(member).read()
            if str(path.parent) == "apps/web/e2e" and path.name.endswith(".spec.ts"):
                specs.add(path.name)
    def regular(path):
        return all(not parent.is_symlink() for parent in [path, *path.parents]) and stat.S_ISREG(path.stat().st_mode)
    summary = {"exit_status": status, "surfaces": {}}
    for surface in ["harness", "normal"]:
        facts = {"report": "unavailable", "failures": []}
        report = mode / surface / "results.json"
        if report.exists() and regular(report) and report.stat().st_size <= 16 * 1024 * 1024:
            try:
                data = json.loads(report.read_text())
                def failures(suites):
                    for suite in suites:
                        for spec in suite.get("specs", []):
                            file = Path(spec.get("file", "")).name
                            line = spec.get("line")
                            if file not in specs or type(line) is not int or not 0 < line < 100000:
                                continue
                            for test in spec.get("tests", []):
                                for result in test.get("results", []):
                                    state = result.get("status")
                                    if state in ["failed", "timedOut", "interrupted"]:
                                        facts["failures"].append({"file": file, "line": line, "status": state})
                        failures(suite.get("suites", []))
                failures(data.get("suites", []))
                facts["report"] = "available"
            except (ValueError, TypeError, AttributeError, RecursionError):
                facts["failures"] = []
        summary["surfaces"][surface] = facts
        artifacts = mode / surface / "artifacts"
        if not artifacts.is_dir() or artifacts.is_symlink():
            continue
        for index, directory in enumerate(sorted(artifacts.iterdir())):
            if not directory.is_dir() or directory.is_symlink():
                continue
            for stem, expected in baselines.items():
                names = [f"{stem}-{kind}.png" for kind in ["expected", "actual", "diff"]]
                # Print and template page comparators write the actual raster without a suffix.
                names.append(f"{stem}.png")
                images = {}
                for name in names:
                    path = directory / name
                    if not path.exists() or not regular(path) or path.stat().st_size > 32 * 1024 * 1024:
                        continue
                    content = pixel_png(path.read_bytes())
                    if content is not None:
                        kind = "actual" if name == f"{stem}.png" else name[len(stem) + 1:-4]
                        images[kind] = content
                if not images:
                    continue
                if "expected" not in images:
                    content = pixel_png(expected)
                    if content is not None:
                        images["expected"] = content
                image_directory = staged / surface / str(index)
                image_directory.mkdir(parents=True, exist_ok=True)
                for kind, content in images.items():
                    (image_directory / f"{stem}-{kind}.png").write_bytes(content)
    (staged / "failure-summary.json").write_text(json.dumps(summary, sort_keys=True) + "\n")
    (staged / "source-metadata.txt").write_text(f"commit={commit}\nmanifest_sha256={manifest_hash}\ntar_sha256={tar_hash}\nbrowser_image_digest={digest}\n")
    staged.rename(destination)
    if status == 0 and mode.name == "update":
        candidate = mode / "candidate-baselines"
        if not candidate.is_dir() or candidate.is_symlink():
            raise ValueError("candidate baseline directory is missing or unsafe")
        with (candidate / "source-metadata.txt").open("x") as metadata:
            metadata.write((destination / "source-metadata.txt").read_text())
finally:
    if staged.exists():
        shutil.rmtree(staged)
