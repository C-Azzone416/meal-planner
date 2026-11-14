import copy
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tarfile
import unittest

spec = importlib.util.spec_from_file_location('boundary', Path(__file__).with_name('private-output-boundary.py'))
b = importlib.util.module_from_spec(spec)
spec.loader.exec_module(b)

CONTEXT = dict(project='prj_fixture', target='preview', source_sha='a'*40, workflow_sha='b'*40, artifact_id=123)


def fixture(entries=None):
    entries = entries or [('config.json', b'{"version":3}'), ('static/index.html', b'<html>synthetic fixture</html>')]
    output = io.BytesIO()
    files = {}
    directories = set()
    for name, data in entries:
        parts = name.split('/')
        directories.update('/'.join(parts[:i]) for i in range(1, len(parts)))
        files[name] = dict(size=len(data), sha256=hashlib.sha256(data).hexdigest(), mode=0o644)
    with tarfile.open(fileobj=output, mode='w', format=tarfile.USTAR_FORMAT) as archive:
        for directory in sorted(directories):
            item = tarfile.TarInfo(directory)
            item.type = tarfile.DIRTYPE
            item.mode = 0o755
            archive.addfile(item)
        for name, data in entries:
            item = tarfile.TarInfo(name)
            item.mode = 0o644
            item.size = len(data)
            archive.addfile(item, io.BytesIO(data))
    raw = output.getvalue()
    receipt = dict(version=1, **CONTEXT, archive_sha256=hashlib.sha256(raw).hexdigest(), files=files, directories=sorted(directories))
    return raw, receipt


class BoundaryTests(unittest.TestCase):
    def test_synthetic_closed_inventory(self):
        raw, receipt = fixture()
        self.assertEqual(b.inspect_archive(raw, receipt, CONTEXT)['files'], 2)

    def test_duplicate_json_keys(self):
        for data in [b'{"a":1,"a":2}', b'{"x":{"a":1,"\\u0061":2}}', b'{"a":[{"z":1,"z":2}]}']:
            with self.subTest(data=data), self.assertRaises(ValueError):
                b.strict_json(data)

    def test_invalid_json(self):
        for data in [b'{"a":NaN}', b'{"a":Infinity}', b'{"a":"\xff"}', b'{}{}', b'[]', b'{"x":'+b'['*40+b'0'+b']'*40+b'}']:
            with self.subTest(data=data), self.assertRaises((ValueError, UnicodeError)):
                b.strict_json(data)

    def test_metadata_at_each_location(self):
        for name in ['config.json', 'functions/api/test.func/.vc-config.json']:
            raw, receipt = fixture([(name, b'{"runtime":"provided.al2023","runtime":"nodejs22.x"}')])
            with self.subTest(name=name), self.assertRaises(ValueError):
                b.inspect_archive(raw, receipt, CONTEXT)

    def test_context_changes(self):
        raw, receipt = fixture()
        for key, value in [('project', 'prj_other'), ('target', 'production'), ('source_sha','c'*40), ('workflow_sha','d'*40), ('artifact_id',124)]:
            changed = {**CONTEXT, key:value}
            with self.subTest(key=key), self.assertRaises(ValueError):
                b.inspect_archive(raw, receipt, changed)

    def test_artifact_identity_type(self):
        raw, receipt = fixture()
        with self.assertRaises(ValueError):
            b.inspect_archive(raw, {**receipt, 'artifact_id':True}, {**CONTEXT, 'artifact_id':True})

    def test_payload_or_digest_changes(self):
        raw, receipt = fixture()
        with self.assertRaises(ValueError):
            b.inspect_archive(raw[:-1]+b'x', receipt, CONTEXT)
        changed = copy.deepcopy(receipt)
        changed['files']['static/index.html']['sha256'] = '0'*64
        with self.assertRaises(ValueError):
            b.inspect_archive(raw, changed, CONTEXT)

    def test_extra_receipt_field(self):
        raw, receipt = fixture()
        with self.assertRaises(ValueError):
            b.inspect_archive(raw, {**receipt,'approved':True}, CONTEXT)

    def test_unknown_directory(self):
        raw, receipt = fixture()
        receipt['directories'].append('routes')
        with self.assertRaises(ValueError):
            b.inspect_archive(raw, receipt, CONTEXT)

    def test_duplicate_archive_entry(self):
        raw, receipt = fixture([('config.json',b'{}'),('config.json',b'{}')])
        with self.assertRaises(ValueError):
            b.inspect_archive(raw, receipt, CONTEXT)

    def test_missing_file(self):
        raw, receipt = fixture()
        receipt['files']['missing.txt'] = dict(size=0,sha256=hashlib.sha256(b'').hexdigest(),mode=0o644)
        with self.assertRaises(ValueError):
            b.inspect_archive(raw, receipt, CONTEXT)

    def test_unsafe_paths(self):
        for name in ['../outside','static/../outside','/absolute','static//a','static/.secret','static/a b','static/é','-option']:
            self.assertFalse(b.path_ok(name), name)

    def test_link_and_special_headers(self):
        raw, receipt = fixture()
        for entry_type in [b'1', b'2', b'3', b'4', b'6', b'x', b'g', b'L']:
            changed = bytearray(raw)
            changed[156:157] = entry_type
            changed[148:156] = b'        '
            changed[148:156] = ('%06o\0 ' % sum(changed[:512])).encode()
            changed = bytes(changed)
            with self.subTest(entry_type=entry_type), self.assertRaises(ValueError):
                b.inspect_archive(changed, {**receipt,'archive_sha256':hashlib.sha256(changed).hexdigest()}, CONTEXT)

    def test_trailing_junk_and_concatenation(self):
        raw, receipt = fixture()
        for changed in [raw+b'x'*512, raw+raw, raw[:512]]:
            with self.assertRaises(ValueError):
                b.inspect_archive(changed, {**receipt,'archive_sha256':hashlib.sha256(changed).hexdigest()}, CONTEXT)

    def test_noncanonical_modes(self):
        raw, receipt = fixture()
        changed = copy.deepcopy(receipt)
        changed['files']['config.json']['mode'] = 0o4755
        with self.assertRaises(ValueError):
            b.inspect_archive(raw, changed, CONTEXT)

if __name__ == '__main__':
    unittest.main()
