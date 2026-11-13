import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

HERE=Path(__file__).parent
spec=importlib.util.spec_from_file_location('layout',HERE/'private-output-layout.py')
l=importlib.util.module_from_spec(spec);spec.loader.exec_module(l)
GOLDEN=(HERE/'fixtures/vercel-65.9.0-synthetic/config.json').read_bytes()
GO=dict(handler='bootstrap',runtime='provided.al2023',runtimeLanguage='go',architecture='x86_64',environment={},supportsWrapper=True)
EXPECTED=dict(enabled=True,environment='preview',origin='https://preview.example.test',appId='meal-planner-preview',buildSha='a'*40,screenshots=False)

def header():
    v=bytearray(64);v[:7]=b'\x7fELF\x02\x01\x01';v[16:18]=(2).to_bytes(2,'little');v[18:20]=(62).to_bytes(2,'little');v[20:24]=(1).to_bytes(4,'little');v[52:54]=(64).to_bytes(2,'little');return bytes(v)

class Tests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.root=Path(self.temp.name).resolve()
        self.put('config.json',GOLDEN)
        self.put('static/framebriar-build.json',json.dumps(EXPECTED).encode())
        self.put('static/assets/index-12345678.js',(EXPECTED['buildSha']+EXPECTED['appId']).encode())
        self.put('static/assets/index-abcdefgh.css',b'body{}')
        self.put('static/index.html',l.INDEX.replace(b'@JS@',b'/assets/index-12345678.js').replace(b'@CSS@',b'/assets/index-abcdefgh.css'))
        for f in l.FUNCTIONS:
            self.put(f'functions/{f}.func/.vc-config.json',json.dumps(GO).encode())
            self.put(f'functions/{f}.func/bootstrap',header(),0o755)
    def tearDown(self):self.temp.cleanup()
    def put(self,name,data,mode=0o644):
        p=self.root/name;p.parent.mkdir(parents=True,exist_ok=True)
        parent=p.parent
        while parent!=self.root:parent.chmod(0o755);parent=parent.parent
        p.write_bytes(data);p.chmod(mode)
    def run_check(self):return l.inspect_layout(self.root,GOLDEN,EXPECTED)
    def test_positive(self):self.assertEqual(len(self.run_check()['published_files']),25)
    def test_diagnostics_not_read(self):
        self.put('builds.json',b'not-json and deliberately not parsed')
        self.put('diagnostics/cli_traces.json',b'private local details not consumed')
        r=self.run_check();self.assertEqual(r['nonpublished_files'],['builds.json','diagnostics/cli_traces.json']);self.assertNotIn('builds.json',r['published_files'])
    def test_unknown_files(self):
        for name in ['routes/unknown.json','static/assets/index-12345678.js.map','functions/api/extra.func/bootstrap','diagnostics/unknown.json']:
            self.put(name,b'bad')
            with self.subTest(name=name),self.assertRaises(ValueError):self.run_check()
            (self.root/name).unlink()
            parent=(self.root/name).parent
            while parent!=self.root and not list(parent.iterdir()):
                parent.rmdir();parent=parent.parent
    def test_empty_directory(self):
        (self.root/'routes').mkdir()
        with self.assertRaises(ValueError):self.run_check()
    def test_missing_function(self):
        (self.root/'functions/api/health.func/bootstrap').unlink()
        with self.assertRaises(ValueError):self.run_check()
    def test_symlink(self):
        p=self.root/'static/assets/index-12345678.js';p.unlink();p.symlink_to(self.root/'config.json')
        with self.assertRaises(ValueError):self.run_check()
    def test_hardlink(self):
        os.link(self.root/'static/index.html',self.root/'alias')
        with self.assertRaises(ValueError):self.run_check()
    def test_fifo(self):
        os.mkfifo(self.root/'pipe')
        with self.assertRaises(ValueError):self.run_check()
    def test_metadata_environment(self):
        self.put('functions/api/health.func/.vc-config.json',json.dumps({**GO,'environment':{'SECRET':'x'}}).encode())
        with self.assertRaises(ValueError):self.run_check()
    def test_architecture_mismatch(self):
        v=bytearray(header());v[18:20]=(183).to_bytes(2,'little');self.put('functions/api/health.func/bootstrap',bytes(v),0o755)
        with self.assertRaises(ValueError):self.run_check()
    def test_provenance(self):
        self.put('static/framebriar-build.json',json.dumps({**EXPECTED,'buildSha':'b'*40}).encode())
        with self.assertRaises(ValueError):self.run_check()
    def test_index_external_or_extra_script(self):
        self.put('static/index.html',l.INDEX+b'<script src="https://outside.invalid"></script>')
        with self.assertRaises(ValueError):self.run_check()
    def test_provenance_duplicate(self):
        self.put('static/framebriar-build.json',b'{"enabled":true,"enabled":false}')
        with self.assertRaises(ValueError):self.run_check()
    def test_capture_marker(self):
        self.put('static/assets/index-12345678.js',(EXPECTED['buildSha']+EXPECTED['appId']+'FRAMEBRIAR_CAPTURE_ACTIVE').encode())
        with self.assertRaises(ValueError):self.run_check()
    def test_source_hardlinks_are_independently_staged(self):
        with tempfile.TemporaryDirectory() as other:
            alias=Path(other).resolve()/'outside-copy'
            source=self.root/'static/assets/index-12345678.js'
            os.link(source,alias)
            self.put('diagnostics/cli_traces.json',b'not published')
            with self.assertRaises(ValueError):self.run_check()
            result=l.stage_layout(self.root,Path(other).resolve(),GOLDEN,EXPECTED)
            staged=Path(result['staged_root'])
            self.assertEqual((staged/'static/assets/index-12345678.js').stat().st_nlink,1)
            self.assertNotEqual(source.stat().st_ino,(staged/'static/assets/index-12345678.js').stat().st_ino)
            self.assertFalse((staged/'diagnostics').exists())
            self.assertEqual(result['excluded_nonpublished_files'],['diagnostics/cli_traces.json'])
    def test_source_change_between_scan_and_copy_rejected(self):
        original=l._inspect
        def change(*args,**kwargs):
            result=original(*args,**kwargs)
            self.put('static/assets/index-12345678.js',b'changed after first scan')
            return result
        with tempfile.TemporaryDirectory() as other,patch.object(l,'_inspect',change):
            with self.assertRaises(ValueError):l.stage_layout(self.root,Path(other).resolve(),GOLDEN,EXPECTED)
    def test_function_source_hardlink_rejected(self):
        with tempfile.TemporaryDirectory() as other:
            os.link(self.root/'functions/api/health.func/bootstrap',Path(other)/'outside')
            with self.assertRaises(ValueError):l.stage_layout(self.root,Path(other).resolve(),GOLDEN,EXPECTED)
    def test_unsafe_mode(self):
        (self.root/'functions/api/health.func/bootstrap').chmod(0o4755)
        with self.assertRaises(ValueError):self.run_check()

if __name__=='__main__':unittest.main()
