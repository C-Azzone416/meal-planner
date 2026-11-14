"""Narrow metadata schema groundwork; no exporter/consumer is enabled.

Expected config bytes must be a reviewed, trusted golden fixture. Reading a
second copy from the target checkout would not establish a trust boundary.
"""
import importlib.util
from pathlib import Path

_spec = importlib.util.spec_from_file_location('private_output_boundary', Path(__file__).with_name('private-output-boundary.py'))
_boundary = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_boundary)
strict_json = _boundary.strict_json


def _config(value):
    if set(value) != {'version', 'daemons', 'routes', 'crons'}:
        raise ValueError('unsupported config fields')
    if type(value['version']) is not int or value['version'] != 3 or type(value['daemons']) is not dict or value['daemons'] or type(value['crons']) is not list or value['crons']:
        raise ValueError('unsupported config services')
    routes = value['routes']
    if not isinstance(routes, list) or not 1 <= len(routes) <= 128:
        raise ValueError('route boundary')
    for route in routes:
        if not isinstance(route, dict):
            raise ValueError('route object')
        if set(route) == {'handle'} and route['handle'] in ('filesystem', 'error', 'miss'):
            continue
        if set(route) not in ({'src', 'dest', 'check'}, {'src', 'status'}, {'src', 'dest', 'status'}):
            # No headers/transforms/has/missing/methods/continue/override/middleware
            # or new semantics are admitted, even when a fixture supplies them.
            raise ValueError('unsupported route semantics')
        if not isinstance(route['src'], str) or not 1 <= len(route['src']) <= 512 or any(ord(c) < 32 for c in route['src']):
            raise ValueError('source boundary')
        if 'dest' in route and (not isinstance(route['dest'], str) or not route['dest'].startswith('/') or route['dest'].startswith('//') or len(route['dest']) > 512 or any(ord(c) < 32 for c in route['dest'])):
            raise ValueError('destination boundary')
        if 'check' in route and route['check'] is not True:
            raise ValueError('route check')
        if 'status' in route and (type(route['status']) is not int or route['status'] != 404):
            raise ValueError('route status')
    return value


def validate_config(raw, trusted_golden_raw):
    actual = _config(strict_json(raw))
    expected = _config(strict_json(trusted_golden_raw))
    if actual != expected:
        raise ValueError('config differs from trusted golden')
    return actual


def validate_materialized_go_config(raw):
    actual = strict_json(raw)
    # The source-level x86_64 override is supported by the pinned CLI and
    # matches the observed legacy Go builder binary. The default ARM64 fixture
    # remains a negative regression; do not silently repair emitted metadata.
    expected = dict(handler='bootstrap', runtime='provided.al2023', runtimeLanguage='go',
                    architecture='x86_64', environment={}, supportsWrapper=True)
    if actual != expected or any(type(actual.get(k)) is not type(v) for k,v in expected.items()):
        raise ValueError('unsupported materialized Go config')
    return actual


def validate_function_binary(config_raw, binary):
    """Bind the observed metadata architecture to the ELF64 LE header.

    This checks a necessary compatibility condition, not executable validity,
    safety, or runtime behavior. Tests may pass just a synthetic 64-byte header;
    the eventual exporter must supply and separately validate the entire binary.
    """
    config = validate_materialized_go_config(config_raw)
    if not isinstance(binary, bytes) or len(binary) < 64 or len(binary) > _boundary.MAX_FILE:
        raise ValueError('ELF boundary')
    if binary[:7] != b'\x7fELF\x02\x01\x01':
        raise ValueError('expected ELF64 little-endian version1')
    if int.from_bytes(binary[16:18], 'little') != 2 or int.from_bytes(binary[20:24], 'little') != 1 or int.from_bytes(binary[52:54], 'little') != 64:
        raise ValueError('unsupported ELF header')
    machine = int.from_bytes(binary[18:20], 'little')
    expected_machine = {'x86_64': 62}[config['architecture']]
    if machine != expected_machine:
        raise ValueError('ELF architecture differs from function metadata')
    return config
