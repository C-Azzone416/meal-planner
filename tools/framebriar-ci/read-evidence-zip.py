"""Parse one bounded metadata file in memory. Never extract or execute archive files."""
import io
import json
import stat
import struct
import sys
import zipfile

MAX_ARCHIVE = 131072
MAX_JSON = 16384

def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError('duplicate key')
        result[key] = value
    return result

try:
    raw = sys.stdin.buffer.read(MAX_ARCHIVE + 1)
    if len(raw) > MAX_ARCHIVE or len(raw) < 22 or not raw.startswith(b'PK\x03\x04'):
        raise ValueError('archive boundary')
    # No trailing content, archive comment, split ZIP or ZIP64 ambiguity.
    eocd = raw[-22:]
    if eocd[:4] != b'PK\x05\x06':
        raise ValueError('archive ending')
    _, disk, directory_disk, count_disk, count, directory_size, directory_offset, comment = struct.unpack('<4s4H2LH', eocd)
    if disk or directory_disk or count_disk != 1 or count != 1 or comment or directory_offset + directory_size != len(raw) - 22:
        raise ValueError('archive directory')
    with zipfile.ZipFile(io.BytesIO(raw)) as archive:
        entries = archive.infolist()
        if len(entries) != 1:
            raise ValueError('entry count')
        item = entries[0]
        mode = item.external_attr >> 16
        if item.filename != 'evidence.json' or item.orig_filename != 'evidence.json' or item.is_dir() or item.header_offset != 0:
            raise ValueError('entry name')
        if stat.S_IFMT(mode) not in (0, stat.S_IFREG) or item.flag_bits & 1:
            raise ValueError('entry mode')
        if item.compress_type not in (zipfile.ZIP_STORED, zipfile.ZIP_DEFLATED) or not 0 < item.file_size <= MAX_JSON or item.compress_size > MAX_ARCHIVE:
            raise ValueError('entry size')
        # One local entry only, with no unlisted entries/junk before the directory.
        signature, version, flags, method, _, _, crc, compressed, expanded, name_len, extra_len = struct.unpack('<4s5H3L2H', raw[:30])
        if flags != item.flag_bits or flags & ~0x808 or method != item.compress_type or raw[30:30 + name_len] != b'evidence.json':
            raise ValueError('local header')
        end = 30 + name_len + extra_len + item.compress_size
        if flags & 8:
            descriptor = raw[end:directory_offset]
            if descriptor.startswith(b'PK\x07\x08'):
                descriptor = descriptor[4:]
            if len(descriptor) != 12 or struct.unpack('<3L', descriptor) != (item.CRC, item.compress_size, item.file_size):
                raise ValueError('data descriptor')
        elif end != directory_offset or (crc, compressed, expanded) != (item.CRC, item.compress_size, item.file_size):
            raise ValueError('local entry boundary')
        with archive.open(item) as stream:
            data = stream.read(MAX_JSON + 1)
            if len(data) != item.file_size or len(data) > MAX_JSON or stream.read(1):
                raise ValueError('decompression bound')
        value = json.loads(data.decode('utf-8'), object_pairs_hook=unique_object, parse_constant=lambda _: (_ for _ in ()).throw(ValueError('nonfinite')))
        if not isinstance(value, dict):
            raise ValueError('metadata object')
        sys.stdout.write(json.dumps(value, separators=(',', ':'), allow_nan=False))
except Exception:
    print('Invalid evidence archive', file=sys.stderr)
    sys.exit(1)
