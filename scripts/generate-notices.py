"""Collect upstream notices for compiled Go dependencies and production web dependencies.
Run with Python 3, after `npm --prefix web ci`, before packaging a release.
"""
import argparse
import json
import pathlib
import subprocess

parser=argparse.ArgumentParser()
parser.add_argument('--go',default='go',help='Go executable')
args=parser.parse_args()
root=pathlib.Path(__file__).resolve().parent.parent
sections=[
    'Meshport third-party notices\n\n'
    'The following texts are copied from upstream dependencies included in Meshport binaries.\n'
    'They describe third-party code only and do not grant a license to Meshport itself.\n'
    'Mihomo is downloaded separately by the installer; its source and licensing are available at\n'
    'https://github.com/MetaCubeX/mihomo\n'
]

def add_notices(name,version,directory):
    files=sorted(p for p in directory.iterdir() if p.is_file() and any(marker in p.name.lower() for marker in ['license','copying','notice','copyright','patents']))
    if not files:
        raise RuntimeError('No upstream license found for '+name)
    text='\n'+'='*78+'\n'+(name+' '+version).strip()+'\n'+'='*78+'\n'
    for file in files:
        text+='\n--- '+file.name+' ---\n'+file.read_text(encoding='utf-8',errors='strict').strip()+'\n'
    sections.append(text)

go_root=pathlib.Path(subprocess.check_output([args.go,'env','GOROOT'],cwd=root,text=True).strip())
add_notices('Go runtime','',go_root)
seen=set()
for platform in ['windows','linux']:
    import os
    env=dict(os.environ,GOOS=platform,GOARCH='amd64',CGO_ENABLED='0')
    listing=subprocess.check_output([args.go,'list','-deps','-f','{{if .Module}}{{.Module.Path}}|{{.Module.Version}}|{{.Module.Dir}}{{end}}','./cmd/...'],cwd=root,text=True,env=env)
    for line in sorted(set(listing.splitlines())):
        if not line.strip(): continue
        name,version,directory=line.split('|',2)
        if not version or (name,version) in seen:continue
        add_notices(name,version,pathlib.Path(directory));seen.add((name,version))

modules=root/'web'/'node_modules'
def visit(directory):
    package=directory/'package.json'
    if not package.exists():raise RuntimeError('Missing installed package at '+str(directory))
    info=json.loads(package.read_text(encoding='utf-8'))
    key=(info['name'],info['version'])
    if key in seen:return
    seen.add(key);add_notices(info['name'],info['version'],directory)
    for dependency in sorted(info.get('dependencies',{})):
        nested=directory/'node_modules'/dependency
        visit(nested if (nested/'package.json').exists() else modules/dependency)

manifest=json.loads((root/'web'/'package.json').read_text(encoding='utf-8'))
for dependency in sorted(manifest['dependencies']):visit(modules/dependency)
(root/'THIRD_PARTY_NOTICES.txt').write_text('\n'.join(sections),encoding='utf-8',newline='\n')
print('Wrote THIRD_PARTY_NOTICES.txt')
