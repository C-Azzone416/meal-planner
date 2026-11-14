"""Inert, closed inventory inspection for the observed Meal Planner output.

No archive, receipt, extraction, upload or deployment is produced. Expected
provenance and golden metadata MUST be supplied by trusted reviewed code, never
the target checkout. Hashes and marker presence are not executable safety proofs.
"""
import hashlib
import importlib.util
import os
from pathlib import Path
import re
import stat
import tempfile
from urllib.parse import urlsplit

_spec=importlib.util.spec_from_file_location('private_output_schema',Path(__file__).with_name('private-output-schema.py'))
schema=importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(schema)
FUNCTIONS=frozenset(('api/auth-config/index','api/auth-proxy/index','api/auth/authorize/index','api/auth/before-create/index','api/feedback/connect/index','api/feedback/disconnect/index','api/feedback/reports/index','api/feedback/status/index','api/health','api/recipes/index'))
NONPUBLISHED=frozenset(('builds.json','diagnostics/cli_traces.json','diagnostics/deploy-manifest.json','diagnostics/project-manifest.json'))
ASSET=re.compile(r'static/assets/index-[A-Za-z0-9_-]{8}\.(js|css)\Z')
MAX_FILE=64*1024*1024
MAX_TOTAL=128*1024*1024
MAX_ENTRIES=128
INDEX=b'''<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>Meal Planner</title>
    <script type="module" crossorigin src="@JS@"></script>
    <link rel="stylesheet" crossorigin href="@CSS@">
  </head>
  <body>
    <div id="root"></div>
  </body>
</html>
'''


def expected_provenance(value):
    if not isinstance(value,dict) or set(value)!={'enabled','environment','origin','appId','buildSha','screenshots'}:
        raise ValueError('provenance fields')
    if type(value['enabled']) is not bool or type(value['screenshots']) is not bool or (value['screenshots'] and not value['enabled']):
        raise ValueError('provenance flags')
    if value['environment']!='preview' or value['appId']!='meal-planner-preview' or not isinstance(value['buildSha'],str) or not re.fullmatch('[0-9a-f]{40}',value['buildSha']):
        raise ValueError('provenance target')
    origin=value['origin']
    if not isinstance(origin,str):raise ValueError('origin type')
    url=urlsplit(origin)
    if url.scheme!='https' or not url.hostname or url.username or url.password or url.path or url.query or url.fragment or '*' in origin or any(ord(c)<33 or ord(c)>126 for c in origin):
        raise ValueError('exact HTTPS origin required')
    return value


def _regular(info, allow_links=False):
    return stat.S_ISREG(info.st_mode) and (info.st_nlink>=1 if allow_links else info.st_nlink==1) and stat.S_IMODE(info.st_mode) in (0o644,0o755) and 0<=info.st_size<=MAX_FILE


def _read(path, before, limit=MAX_FILE, dir_fd=None, allow_links=False):
    if before.st_size>limit:raise ValueError('file limit')
    fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK,dir_fd=dir_fd)
    try:
        current=os.fstat(fd)
        identity=lambda s:(s.st_dev,s.st_ino,s.st_size,s.st_mode,s.st_nlink,s.st_mtime_ns,s.st_ctime_ns)
        if not _regular(current,allow_links) or identity(current)!=identity(before):raise ValueError('file changed before read')
        with os.fdopen(fd,'rb',closefd=False) as stream:data=stream.read(limit+1)
        if len(data)!=current.st_size or identity(os.fstat(fd))!=identity(current):raise ValueError('file changed during read')
        return data
    finally:os.close(fd)


def _inspect_layout(root, trusted_config_raw, expected, root_fd, held, source_links=False):
    expected_provenance(expected)
    root=Path(root)
    if not root.is_absolute() or root.resolve()!=root or not stat.S_ISDIR(root.lstat().st_mode):raise ValueError('physical absolute output root required')
    files={};locations={};directories=set();total=0
    def walk(directory,prefix=""):
        nonlocal total
        with os.scandir(directory) as entries:
            for entry in entries:
                relative=prefix+entry.name
                if len(files)+len(directories)>=MAX_ENTRIES or not schema._boundary.path_ok(relative):raise ValueError('entry boundary')
                info=entry.stat(follow_symlinks=False)
                if stat.S_ISDIR(info.st_mode):
                    if stat.S_IMODE(info.st_mode)!=0o755:raise ValueError('directory mode')
                    child=os.open(entry.name,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW,dir_fd=directory)
                    held.append(child)
                    opened=os.fstat(child)
                    if (opened.st_dev,opened.st_ino,opened.st_mode)!=(info.st_dev,info.st_ino,info.st_mode):raise ValueError('directory changed during walk')
                    directories.add(relative);walk(child,relative+'/')
                elif _regular(info,source_links and relative.startswith("static/")):
                    total+=info.st_size
                    if total>MAX_TOTAL:raise ValueError('tree byte limit')
                    files[relative]=info;locations[relative]=(directory,entry.name)
                else:raise ValueError('unsupported file type or mode')
    walk(root_fd)
    js=[name for name in files if ASSET.fullmatch(name) and name.endswith('.js')]
    css=[name for name in files if ASSET.fullmatch(name) and name.endswith('.css')]
    if len(js)!=1 or len(css)!=1:raise ValueError('exactly one observed JS and CSS asset required')
    publish={'config.json','static/index.html','static/framebriar-build.json',*js,*css}
    for function in FUNCTIONS:publish.update((f'functions/{function}.func/.vc-config.json',f'functions/{function}.func/bootstrap'))
    if not publish.issubset(files) or set(files)-publish-NONPUBLISHED:raise ValueError('unknown or missing output file')
    required_dirs=set()
    for name in files:
        parts=name.split('/');required_dirs.update('/'.join(parts[:i]) for i in range(1,len(parts)))
    if directories!=required_dirs:raise ValueError('unknown or empty directory')
    # Diagnostics are inspected for type/mode/size only and never read or hashed.
    data={name:_read(locations[name][1],files[name],schema._boundary.MAX_METADATA if name.endswith('.json') or name=='static/index.html' else MAX_FILE,dir_fd=locations[name][0],allow_links=source_links and name.startswith("static/")) for name in publish}
    schema.validate_config(data['config.json'],trusted_config_raw)
    actual=schema.strict_json(data['static/framebriar-build.json'])
    expected_provenance(actual)
    if actual!=expected:raise ValueError('public provenance mismatch')
    template=INDEX.replace(b'@JS@',('/'+js[0].removeprefix('static/')).encode()).replace(b'@CSS@',('/'+css[0].removeprefix('static/')).encode())
    if data['static/index.html']!=template:raise ValueError('index differs from reviewed template and assets')
    for function in FUNCTIONS:
        prefix=f'functions/{function}.func/'
        if stat.S_IMODE(files[prefix+'bootstrap'].st_mode)!=0o755:raise ValueError('bootstrap must be executable')
        schema.validate_function_binary(data[prefix+'.vc-config.json'],data[prefix+'bootstrap'])
    for marker in (expected['buildSha'],expected['appId']):
        if marker.encode() not in data[js[0]]:raise ValueError('missing public JS provenance marker')
    capture=b'FRAMEBRIAR_CAPTURE_ACTIVE' in data[js[0]]
    if capture!=expected['screenshots']:raise ValueError('capture marker mismatch')
    return {'published_files':{name:{'size':len(data[name]),'mode':stat.S_IMODE(files[name].st_mode),'sha256':hashlib.sha256(data[name]).hexdigest()} for name in sorted(publish)},'published_directories':sorted({p for name in publish for p in ['/'.join(name.split('/')[:i]) for i in range(1,len(name.split('/')))]}),'nonpublished_files':sorted(set(files)&NONPUBLISHED)}


def _inspect(root, trusted_config_raw, expected, source_links=False):
    root=Path(root)
    if not root.is_absolute() or root.resolve()!=root:raise ValueError('physical absolute output root required')
    held=[]
    try:
        fd=os.open(root,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
        held.append(fd)
        return _inspect_layout(root,trusted_config_raw,expected,fd,held,source_links)
    finally:
        for fd in reversed(held):os.close(fd)


def inspect_layout(root, trusted_config_raw, expected):
    return _inspect(root,trusted_config_raw,expected)


def stage_layout(source, staging_parent, trusted_config_raw, expected):
    """Copy only an accepted inventory into new independent private staging.

    Only observed static source hardlinks are accepted; published files must be
    independent. This is not a substitute for ending the untrusted build job.
    A real exporter still requires a clean process/job/archive handoff.
    """
    inventory=_inspect(source,trusted_config_raw,expected,source_links=True)
    source=Path(source);parent=Path(staging_parent)
    if not parent.is_absolute() or parent.resolve()!=parent or not parent.is_dir():raise ValueError('physical staging parent required')
    target=Path(tempfile.mkdtemp(prefix='framebriar-validated-',dir=parent))
    source_fd=os.open(source,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
    try:
        for directory in sorted(inventory['published_directories'],key=lambda p:(p.count('/'),p)):
            (target/directory).mkdir(mode=0o755)
            (target/directory).chmod(0o755)
        for name,spec in inventory['published_files'].items():
            held=[];directory=source_fd
            try:
                parts=name.split('/')
                for component in parts[:-1]:
                    directory=os.open(component,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW,dir_fd=directory);held.append(directory)
                info=os.stat(parts[-1],dir_fd=directory,follow_symlinks=False)
                raw=_read(parts[-1],info,dir_fd=directory,allow_links=name.startswith('static/'))
                if len(raw)!=spec['size'] or hashlib.sha256(raw).hexdigest()!=spec['sha256'] or stat.S_IMODE(info.st_mode)!=spec['mode']:raise ValueError('source changed after inspection')
                fd=os.open(target/name,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,spec['mode'])
                with os.fdopen(fd,'wb') as stream:
                    stream.write(raw);stream.flush();os.fchmod(stream.fileno(),spec['mode'])
            finally:
                for fd in reversed(held):os.close(fd)
    finally:os.close(source_fd)
    checked=inspect_layout(target,trusted_config_raw,expected)
    if checked['published_files']!=inventory['published_files']:raise ValueError('staging inventory changed')
    return {'staged_root':str(target),'inventory':checked,'excluded_nonpublished_files':inventory['nonpublished_files']}
