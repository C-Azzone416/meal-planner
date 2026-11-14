import importlib.util
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
HERE=Path(__file__).parent

def load(name,file):
    spec=importlib.util.spec_from_file_location(name,HERE/file)
    module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module);return module
fixture=load('fixture','private-output-layout_test.py')
p=load('pack','package-reviewed-output.py')

class Packaging(unittest.TestCase):
    def setUp(self):
        self.fixture=fixture.Tests();self.fixture.setUp()
        self.root=self.fixture.root
        expected={**fixture.EXPECTED,'origin':'https://athome-meals-dev.vercel.app','screenshots':True}
        self.fixture.put('static/framebriar-build.json',json.dumps(expected).encode())
        self.fixture.put('static/assets/index-12345678.js',(expected['buildSha']+expected['appId']+'FRAMEBRIAR_CAPTURE_ACTIVE').encode())
        self.destination=tempfile.TemporaryDirectory();self.out=Path(self.destination.name).resolve()
    def tearDown(self):self.fixture.tearDown();self.destination.cleanup()
    def test_selected_only_and_modes(self):
        self.fixture.put('diagnostics/cli_traces.json',b'not publishable')
        digest=p.package(self.root,self.out,'a'*40)
        with tarfile.open(self.out/'build-output.tar') as tar:
            self.assertEqual(len(tar.getmembers()),27)
            self.assertTrue(all(i.isfile() for i in tar.getmembers()))
            self.assertFalse(any('diagnostics' in i.name or 'project.json' in i.name for i in tar.getmembers()))
            self.assertEqual(tar.getmember('.vercel/output/functions/api/health.func/bootstrap').mode,0o755)
        self.assertEqual(json.loads((self.out/'manifest.json').read_text())['build_output_tar_sha256'],digest)
    def test_wrong_sha_rejected_before_upload(self):
        with self.assertRaises(ValueError):p.package(self.root,self.out,'b'*40)
        self.assertEqual(list(self.out.iterdir()),[])
    def test_raw_extra_file_rejected(self):
        self.fixture.put('sdk-source.js',b'raw source')
        with self.assertRaises(ValueError):p.package(self.root,self.out,'a'*40)
        self.assertEqual(list(self.out.iterdir()),[])
    def test_nonempty_output_rejected(self):
        (self.out/'old.tar').write_bytes(b'old')
        with self.assertRaises(ValueError):p.package(self.root,self.out,'a'*40)
    def test_sourcemap_rejected(self):
        self.fixture.put('static/assets/index-12345678.js.map',b'{}')
        with self.assertRaises(ValueError):p.package(self.root,self.out,'a'*40)

if __name__=='__main__':unittest.main()
