import importlib.util
import json
from pathlib import Path
import unittest

HERE = Path(__file__).parent
spec = importlib.util.spec_from_file_location('schema', HERE/'private-output-schema.py')
s = importlib.util.module_from_spec(spec)
spec.loader.exec_module(s)
GOLDEN = (HERE/'fixtures/vercel-65.9.0-synthetic/config.json').read_bytes()
GO = dict(handler='bootstrap',runtime='provided.al2023',runtimeLanguage='go',architecture='arm64',environment={},supportsWrapper=True)
b = lambda value: json.dumps(value).encode()

class SchemaTests(unittest.TestCase):
    def test_actual_pinned_cli_synthetic_config(self):
        s.validate_config(GOLDEN, GOLDEN)

    def test_actual_materialized_go_shape(self):
        s.validate_materialized_go_config((HERE/"fixtures/vercel-65.9.0-synthetic/go-config.json").read_bytes())

    def test_response_mutations_even_in_golden(self):
        for key,val in [('headers',{'x-test':'$env.SECRET'}),('transforms',[{'type':'response.headers','op':'set','target':{'key':'x-secret'},'args':'$1'}]),('continue',True),('has',[])]:
            value=json.loads(GOLDEN)
            value['routes'][1][key]=val
            with self.subTest(key=key),self.assertRaises(ValueError):
                s.validate_config(b(value),b(value))

    def test_exact_routes_required(self):
        value=json.loads(GOLDEN)
        value['routes'][1]['dest']='/other'
        with self.assertRaises(ValueError): s.validate_config(b(value),GOLDEN)
        value=json.loads(GOLDEN)
        value['routes'].reverse()
        with self.assertRaises(ValueError): s.validate_config(b(value),GOLDEN)

    def test_unsupported_fields_and_services(self):
        for key,val in [('images',{}),('overrides',{}),('cache',[]),('daemons',{'x':{}}),('crons',[{'path':'/x','schedule':'* * * * *'}])]:
            value=json.loads(GOLDEN);value[key]=val
            with self.subTest(key=key),self.assertRaises(ValueError):s.validate_config(b(value),b(value))

    def test_external_route_and_status(self):
        for key,val in [('dest','https://external.invalid'),('dest','//external.invalid'),('status',302),('check',1)]:
            value=json.loads(GOLDEN);value['routes'][1][key]=val
            with self.subTest(key=key,val=val),self.assertRaises(ValueError):s.validate_config(b(value),b(value))

    def test_go_rejects_extra_semantics(self):
        for key,val in [('filePathMap',{}),('environment',{'TOKEN':'x'}),('regions',['iad1']),('runtime','nodejs22.x'),('architecture','x86_64'),('supportsWrapper',1),('handler','../../outside'),('maxDuration',300)]:
            with self.subTest(key=key),self.assertRaises(ValueError):s.validate_materialized_go_config(b({**GO,key:val}))

    def test_elf_architecture_binding(self):
        header=bytearray(64)
        header[:7]=b"\x7fELF\x02\x01\x01"
        header[16:18]=(2).to_bytes(2,"little")
        header[18:20]=(183).to_bytes(2,"little")
        header[20:24]=(1).to_bytes(4,"little")
        header[52:54]=(64).to_bytes(2,"little")
        s.validate_function_binary(b(GO),bytes(header))
        for offset,value in [(18,62),(4,1),(5,2),(6,0),(16,1),(20,0),(52,0)]:
            changed=bytearray(header);changed[offset]=value
            with self.subTest(offset=offset),self.assertRaises(ValueError):
                s.validate_function_binary(b(GO),bytes(changed))
        with self.assertRaises(ValueError):s.validate_function_binary(b(GO),bytes(header[:63]))

    def test_duplicate_metadata_before_semantics(self):
        for value in [b'{"version":3,"version":3}',b'{"environment":{},"\\u0065nvironment":{}}']:
            with self.assertRaises(ValueError):s.validate_materialized_go_config(value)
            with self.assertRaises(ValueError):s.validate_config(value,GOLDEN)

if __name__ == '__main__':unittest.main()
