"""Destructive only to Meshport test data. Run on a fresh server installed from this repository.
Uses local fixture subscriptions and assertions, never logs credentials or config bodies.
"""
import hashlib
import http.cookiejar
import http.server
import json
import os
import socket
import subprocess
import threading
import time
import urllib.error
import urllib.request

PASSWORD = os.environ.get('MESHPORT_TEST_PASSWORD', 'meshport-test-password-2026')
BASE = 'http://127.0.0.1:8888'
jar = http.cookiejar.CookieJar()
client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar), urllib.request.ProxyHandler({}))
def command(*args):
    return subprocess.check_output(args, text=True, stderr=subprocess.STDOUT).strip()
def api(path, method='GET', body=None, expected=200):
    request = urllib.request.Request(BASE + '/api' + path, data=json.dumps(body).encode() if body is not None else (b'{}' if method != 'GET' else None), method=method, headers={'Content-Type':'application/json','X-Meshport-Request':'1'})
    try:
        with client.open(request, timeout=90) as response:
            status, value = response.status, json.load(response)
    except urllib.error.HTTPError as exc:
        status, value = exc.code, json.load(exc)
    assert status == expected, (path, status, value)
    return value
def state(): return api('/state')
def settings(**changes):
    st = state(); config = st['settings']; config.pop('has_proxy_password', None); config.update(changes)
    return api('/settings','PUT',{'settings':config,'revision':st['revision']})
def runtime_sha(): return command('sudo','sha256sum','/var/lib/meshport/runtime.yaml').split()[0]
def check(label): print('PASS:', label, flush=True)

NODE_YAML = 'proxies:\n  - {name: first-direct, type: direct}\n  - {name: second-direct, type: direct}\n'
FULL_YAML = 'proxies:\n  - {name: complete-direct, type: direct}\nproxy-groups:\n  - {name: Choice, type: select, proxies: [complete-direct, DIRECT]}\nrules: ["MATCH,Choice"]\nport: 7777\nexternal-controller: 0.0.0.0:9999\ntun: {enable: true}\n'
fixture = NODE_YAML
class Fixture(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        content = fixture.encode(); self.send_response(200); self.send_header('Content-Length',str(len(content))); self.end_headers();self.wfile.write(content)
    def log_message(self,*args): pass
source = http.server.HTTPServer(('127.0.0.1',18080),Fixture)
threading.Thread(target=source.serve_forever,daemon=True).start()

assert command('sudo','systemctl','is-active','meshport') == 'active'
assert api('/auth')['initialized'] is False
api('/state', expected=401)
token = command('sudo','cat','/var/lib/meshport/init-token')
api('/setup','POST',{'token':token,'password':PASSWORD})
api('/setup','POST',{'token':token,'password':PASSWORD},expected=403)
assert not state()['service']['running'] and not state()['settings']['tun']
check('first-run auth, token invalidation, proxy and TUN off')

api('/subscriptions','POST',{'name':'验收节点订阅','url':'http://127.0.0.1:18080/nodes?token=test-secret'})
api('/subscriptions','POST',{'name':'验收完整配置','content':FULL_YAML})
st = state(); nodes, full = [s['id'] for s in st['subscriptions']]
assert 'test-secret' not in json.dumps(st)
api('/subscriptions/'+nodes+'/activate','POST')
settings(http_port=17890,socks_port=17891)
api('/service','POST',{'action':'start'})
assert state()['service']['running']
p = api('/nodes')['proxies']; assert 'PROXY' in p
api('/nodes/select','PUT',{'group':'PROXY','name':'second-direct'})
settings(mode='global',autostart=True)
api('/service','POST',{'action':'restart'})
assert api('/nodes')['proxies']['PROXY']['now'] == 'second-direct'
assert state()['settings']['mode'] == 'global' and state()['service']['autostart']
check('node template, real core start, selection/mode/ports/autostart persistence')

command('sudo','systemctl','restart','meshport')
time.sleep(2)
assert state()['service']['running']
assert api('/nodes')['proxies']['PROXY']['now'] == 'second-direct'
command('sudo','systemctl','kill','--signal=SIGKILL','meshport-mihomo')
time.sleep(2)
assert not state()['service']['running']
api('/nodes',expected=409)
api('/service','POST',{'action':'start'})
check('manager restart leaves core running; core crash leaves manager accessible')

before = runtime_sha(); fixture='proxies: [{name: broken, type: does-not-exist}]\n'
api('/subscriptions/'+nodes+'/update','POST',expected=400)
assert runtime_sha() == before and state()['service']['running']
api('/subscriptions/'+nodes,'DELETE',expected=400)
fixture='proxies:\n  - {name: first-direct, type: direct}\n'
api('/subscriptions/'+nodes+'/update','POST')
assert 'second-direct' not in api('/nodes')['proxies']['PROXY']['all']
assert state()['notice']
api('/subscriptions/'+full+'/activate','POST')
assert 'Choice' in api('/nodes')['proxies']
assert state()['settings']['http_port'] == 17890
check('failed update retains config; deleted choice cleared; full subscription routing preserved')

occupied_proxy=socket.socket();occupied_proxy.bind(('127.0.0.1',17899));occupied_proxy.listen()
st=state();config=st['settings'];config.pop('has_proxy_password',None);config['http_port']=17899
before=runtime_sha();api('/settings','PUT',{'settings':config,'revision':st['revision']},expected=400)
assert runtime_sha()==before and state()['settings']['http_port']==17890;occupied_proxy.close()
check('proxy port conflict preserves running core and previous configuration')

occupied=socket.socket();occupied.bind(('127.0.0.1',18889));occupied.listen()
st=state();config=st['settings'];config.pop('has_proxy_password',None);config['web_port']=18889
api('/settings','PUT',{'settings':config,'revision':st['revision']},expected=400)
assert state()['settings']['web_port']==8888;occupied.close()
settings(web_port=8889);BASE='http://127.0.0.1:8889';assert state()['settings']['web_port']==8889
time.sleep(6)
with socket.socket() as sock: assert sock.connect_ex(('127.0.0.1',8888)) != 0
settings(web_port=8888);BASE='http://127.0.0.1:8888';assert state()['service']['running']
check('Web conflict leaves listener intact; migration opens new port and closes old port')

settings(allow_lan=True,proxy_user='test-proxy',proxy_password='test-proxy-password')
request=urllib.request.Request('http://example.com/')
proxy=urllib.request.build_opener(urllib.request.ProxyHandler({'http':'http://127.0.0.1:17890'}))
try: proxy.open(request,timeout=10); raise AssertionError('unauthenticated proxy accepted')
except urllib.error.HTTPError as exc: assert exc.code==407
settings(allow_lan=False)
check('external proxy requires independent authentication')

settings(mode='rule',tun=True)
assert state()['pending_tun'] and state()['settings']['tun']
command('sudo','systemctl','is-active','meshport-tun-rollback.timer')
command('getent','hosts','example.com')
assert 'meshport0' in command('ip','link','show')
check('TUN enabled, DNS and SSH connection usable; waiting for independent rollback')
command('sudo','systemctl','stop','meshport')
for _ in range(40):
    raw=command('sudo','python3','-c',"import sqlite3,json; s=json.loads(sqlite3.connect('/var/lib/meshport/meshport.db').execute('SELECT value FROM state').fetchone()[0]); print(s['pending_tun'] is None and not s['settings']['tun'])")
    if raw=='True': break
    time.sleep(2)
else: raise AssertionError('independent TUN rollback timed out')
command('sudo','systemctl','start','meshport');time.sleep(2)
assert not state()['settings']['tun'] and state()['service']['running']
assert 'meshport0' not in command('ip','link','show')
assert 'lookup 20220' not in command('ip','rule','show')
command('getent','hosts','example.com')
check('TUN timeout restores config with manager stopped and cleans owned routes')

settings(tun=True);api('/tun/confirm','POST');assert not state()['pending_tun']
settings(tun=False);assert 'meshport0' not in command('ip','link','show')
api('/service','POST',{'action':'stop'});settings(autostart=False)
assert state()['service']['running'] is False
assert state()['service']['autostart'] is False
check('TUN confirmation and explicit disable; proxy stopped, autostart disabled')
source.shutdown()
print('SERVER2 ACCEPTANCE COMPLETE',flush=True)
