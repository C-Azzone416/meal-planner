"""Inert, exact-inventory verifier for a future private Preview exporter.

Not a deployment authorizer. The expected inventory MUST come from a separately
verified clean-reporter receipt, never from the archive or target checkout. The
caller must verify workflow/source/chronology, schema readiness and provenance.
No extraction, execution, token use or upload is performed here. No static scan
can prove absence of transformed private source in executable assets.
"""
import hashlib
import io
import json
import re
import stat
import tarfile

MAX_ARCHIVE = 128 * 1024 * 1024
MAX_FILE = 64 * 1024 * 1024
MAX_TOTAL = 128 * 1024 * 1024
MAX_ENTRIES = 4096
MAX_METADATA = 1024 * 1024
HEX = re.compile(r"[0-9a-f]{64}\Z")
SHA = re.compile(r"[0-9a-f]{40}\Z")


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate JSON key")
        result[key] = value
    return result


def strict_json(data):
    if not isinstance(data, bytes) or len(data) > MAX_METADATA:
        raise ValueError("metadata boundary")
    # JSON parser recursion is bounded by Python; reject excessive nesting before
    # decode to bound parsing even for tiny malicious deeply nested documents.
    quoted = escaped = False
    depth = 0
    for char in data:
        if quoted:
            if escaped:
                escaped = False
            elif char == 92:
                escaped = True
            elif char == 34:
                quoted = False
        elif char == 34:
            quoted = True
        elif char in (91, 123):
            depth += 1
            if depth > 32:
                raise ValueError("metadata depth")
        elif char in (93, 125):
            depth -= 1
    value = json.loads(data.decode("utf-8"), object_pairs_hook=unique_object,
                       parse_constant=lambda _: (_ for _ in ()).throw(ValueError("nonfinite")))
    if not isinstance(value, dict):
        raise ValueError("metadata object")
    return value


def path_ok(name):
    # Deliberately no unicode, escapes, whitespace, leading dash or hidden path
    # components except the exact Vercel metadata names used below.
    if not isinstance(name, str) or not name or len(name) > 512:
        return False
    parts = name.split("/")
    return all(re.fullmatch(r"[A-Za-z0-9_][A-Za-z0-9_.-]*|\.vc-config\.json", p)
               and p not in (".", "..") for p in parts)


def receipt_ok(receipt, expected):
    keys = {"version", "project", "target", "source_sha", "workflow_sha",
            "artifact_id", "archive_sha256", "files", "directories"}
    if not isinstance(receipt, dict) or set(receipt) != keys or type(receipt["version"]) is not int or receipt["version"] != 1:
        raise ValueError("receipt schema")
    if set(expected) != {"project", "target", "source_sha", "workflow_sha", "artifact_id"}:
        raise ValueError("expected context schema")
    if expected["target"] != "preview" or not isinstance(expected["project"], str) or not re.fullmatch(r"prj_[A-Za-z0-9]+", expected["project"]):
        raise ValueError("Preview project required")
    if any(not isinstance(expected[k], str) or not SHA.fullmatch(expected[k]) for k in ("source_sha", "workflow_sha")):
        raise ValueError("commit identity")
    if type(expected["artifact_id"]) is not int or expected["artifact_id"] <= 0:
        raise ValueError("artifact identity")
    if any(receipt[k] != v or type(receipt[k]) is not type(v) for k, v in expected.items()):
        raise ValueError("context mismatch")
    if not isinstance(receipt["archive_sha256"], str) or not HEX.fullmatch(receipt["archive_sha256"]):
        raise ValueError("archive digest")
    files, directories = receipt["files"], receipt["directories"]
    if not isinstance(files, dict) or not files or not isinstance(directories, list) or len(files) + len(directories) > MAX_ENTRIES:
        raise ValueError("inventory bound")
    if any(not path_ok(p) for p in [*files, *directories]) or len(set(directories)) != len(directories) or set(files) & set(directories):
        raise ValueError("inventory path")
    # Exact closed layout: unknown/empty directories cannot silently survive.
    required_dirs = set()
    for name, spec in files.items():
        if not isinstance(spec, dict) or set(spec) != {"size", "sha256", "mode"}:
            raise ValueError("file schema")
        if type(spec["size"]) is not int or not 0 <= spec["size"] <= MAX_FILE or spec["mode"] not in (0o644, 0o755) or type(spec["mode"]) is not int:
            raise ValueError("file bounds")
        if not isinstance(spec["sha256"], str) or not HEX.fullmatch(spec["sha256"]):
            raise ValueError("file digest")
        parts = name.split("/")
        required_dirs.update("/".join(parts[:i]) for i in range(1, len(parts)))
    if set(directories) != required_dirs or sum(v["size"] for v in files.values()) > MAX_TOTAL:
        raise ValueError("closed inventory")
    return files, set(directories)


def inspect_archive(raw, receipt, expected):
    """Verify a bounded uncompressed USTAR archive against an authenticated receipt.

    A receipt is necessary, not sufficient: this helper intentionally has no
    built-in 'approved' layout. The future clean reporter must validate actual
    pinned-CLI metadata with a separately reviewed closed schema BEFORE issuing
    it. Receipt authentication cannot be replaced by trusting builder JSON.
    """
    files, directories = receipt_ok(receipt, expected)
    if not isinstance(raw, bytes) or not 1024 <= len(raw) <= MAX_ARCHIVE or len(raw) % 512:
        raise ValueError("archive boundary")
    if hashlib.sha256(raw).hexdigest() != receipt["archive_sha256"]:
        raise ValueError("archive mismatch")
    seen, total = set(), 0
    # Inspect headers ourselves first: tarfile silently accepts concatenation,
    # PAX overrides and end junk. Only ordinary USTAR headers are supported.
    offset = 0
    while offset + 512 <= len(raw):
        block = raw[offset:offset + 512]
        if block == bytes(512):
            if len(raw) - offset < 1024 or any(raw[offset:]):
                raise ValueError("archive ending")
            break
        if block[257:263] != b"ustar\x00" or block[263:265] != b"00" or any(block[345:500]):
            raise ValueError("unsupported header")
        if block[156:157] not in (b"0", b"\x00", b"5") or any(block[157:257]):
            raise ValueError("unsupported entry")
        try:
            size = int(block[124:136].rstrip(b"\x00 ") or b"0", 8)
        except ValueError as error:
            raise ValueError("size encoding") from error
        if size < 0 or size > MAX_FILE:
            raise ValueError("file size")
        offset += 512 + ((size + 511) // 512) * 512
        if offset > len(raw):
            raise ValueError("truncated data")
    else:
        raise ValueError("missing archive ending")
    with tarfile.open(fileobj=io.BytesIO(raw), mode="r:") as archive:
        for item in archive:
            if len(seen) >= MAX_ENTRIES or item.name in seen or not path_ok(item.name):
                raise ValueError("duplicate or invalid entry")
            seen.add(item.name)
            if item.isdir():
                if item.name not in directories or item.size != 0 or item.mode != 0o755:
                    raise ValueError("directory mismatch")
                continue
            if not item.isfile() or item.name not in files:
                raise ValueError("file outside inventory")
            spec = files[item.name]
            if item.mode != spec["mode"] or item.size != spec["size"]:
                raise ValueError("file metadata mismatch")
            total += item.size
            if total > MAX_TOTAL:
                raise ValueError("expanded size")
            stream = archive.extractfile(item)
            data = stream.read(MAX_FILE + 1)
            if len(data) != item.size or hashlib.sha256(data).hexdigest() != spec["sha256"]:
                raise ValueError("file content mismatch")
            # Every recognized JSON metadata location is parsed strictly. Exact
            # schemas remain the clean reporter's separately reviewed duty.
            if item.name == "config.json" or item.name.endswith("/.vc-config.json"):
                strict_json(data)
    if seen != set(files) | directories:
        raise ValueError("incomplete inventory")
    return {"files": len(files), "bytes": total, "archive_sha256": receipt["archive_sha256"]}
