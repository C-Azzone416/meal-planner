"""Package intended compiled output from reviewed source, not arbitrary hostile code.

No network or credentials. This is an integrity/inventory boundary, not a proof
that a reviewed JavaScript bundle cannot disclose transformed private source.
"""
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import tarfile
import tempfile

HERE=Path(__file__).parent
spec=importlib.util.spec_from_file_location('layout',HERE/'private-output-layout.py')
layout=importlib.util.module_from_spec(spec);spec.loader.exec_module(layout)


def package(source,destination,sha):
    if not isinstance(sha,str) or not re.fullmatch('[a-f0-9]{40}',sha):raise ValueError('exact source required')
    source=Path(source).resolve(strict=True);destination=Path(destination).resolve(strict=True)
    if not destination.is_dir() or list(destination.iterdir()):raise ValueError('empty destination required')
    expected=dict(enabled=True,environment='preview',origin='https://athome-meals-dev.vercel.app',appId='meal-planner-preview',buildSha=sha,screenshots=True)
    golden=(HERE/'fixtures/vercel-65.9.0-synthetic/config.json').read_bytes()
    with tempfile.TemporaryDirectory(prefix='reviewed-output-',dir=destination.parent) as scratch:
        staged=layout.stage_layout(source,Path(scratch).resolve(),golden,expected)
        root=Path(staged['staged_root']);inventory=staged['inventory']
        archive=destination/'build-output.tar'
        try:
            with tarfile.open(archive,'x',format=tarfile.USTAR_FORMAT) as tar:
                for name,item in inventory['published_files'].items():
                    info=(root/name).lstat()
                    data=layout._read(root/name,info)
                    if hashlib.sha256(data).hexdigest()!=item['sha256']:raise ValueError('staged bytes changed')
                    entry=tarfile.TarInfo('.vercel/output/'+name)
                    entry.size=len(data);entry.mode=item['mode'];entry.mtime=0
                    tar.addfile(entry,io.BytesIO(data))
            if archive.stat().st_size>layout.MAX_TOTAL+1024*1024:raise ValueError('archive size')
            digest=hashlib.sha256(archive.read_bytes()).hexdigest()
            manifest={'source_sha':sha,'target':'preview','build_output_tar_sha256':digest,'published_files':inventory['published_files']}
            (destination/'manifest.json').write_text(json.dumps(manifest,sort_keys=True)+'\n')
        except BaseException:
            archive.unlink(missing_ok=True)
            (destination/'manifest.json').unlink(missing_ok=True)
            raise
    return digest

if __name__=='__main__':
    import sys
    try:
        if len(sys.argv)!=4:raise ValueError('arguments')
        digest=package(*sys.argv[1:])
        if os.environ.get('GITHUB_OUTPUT'):
            with open(os.environ['GITHUB_OUTPUT'],'a') as output:output.write('output_sha256='+digest+'\n')
        else:print(digest)
    except Exception:
        print('Reviewed Preview output rejected; no deployment authorized.',file=sys.stderr)
        sys.exit(1)
