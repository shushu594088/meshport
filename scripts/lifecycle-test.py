"""Run after acceptance/browser tests, only on a disposable Meshport installation."""
import http.cookiejar
import json
import subprocess
import sys
import time
import urllib.request

package=sys.argv[1]
client=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()),urllib.request.ProxyHandler({}))
def api(path,method='GET',value=None):
    req=urllib.request.Request('http://127.0.0.1:8888/api'+path,method=method,data=json.dumps(value).encode() if value is not None else None,headers={'Content-Type':'application/json','X-Meshport-Request':'1'})
    with client.open(req,timeout=60) as res:return json.load(res)
def run(*args):return subprocess.check_output(args,text=True,stderr=subprocess.STDOUT)
api('/login','POST',{'password':'meshport-test-password-2026'})
before=api('/state');settings=before['settings'];settings.pop('has_proxy_password',None);settings['autostart']=True
api('/settings','PUT',{'settings':settings,'revision':before['revision']})
before=api('/state')
run('sudo','bash',package+'/uninstall.sh')
assert subprocess.call(['test','-e','/usr/bin/meshport'])!=0
assert run('sudo','test','-e','/var/lib/meshport/meshport.db')==''
run('sudo','bash',package+'/install.sh');time.sleep(2)
assert api('/auth')['initialized']
api('/login','POST',{'password':'meshport-test-password-2026'})
after=api('/state')
assert after['settings']==before['settings']
assert after['subscriptions']==before['subscriptions']
assert after['service']['autostart'] and not after['service']['running']
bad=subprocess.run(['sudo','-u','meshport','sudo','-n','/usr/libexec/meshport-helper','arbitrary-command'],capture_output=True)
assert bad.returncode!=0
bad=subprocess.run(['sudo','/usr/libexec/meshport-helper','start','another.service'],capture_output=True)
assert bad.returncode!=0
print('PASS: default uninstall retains password, subscriptions, settings and autostart; helper rejects arbitrary commands and arguments',flush=True)
run('sudo','bash',package+'/uninstall.sh','--purge')
assert subprocess.call(['test','-e','/usr/bin/meshport'])!=0
assert subprocess.call(['sudo','test','-e','/var/lib/meshport'])!=0
assert subprocess.call(['sudo','test','-e','/etc/meshport'])!=0
run('sudo','bash',package+'/install.sh');time.sleep(2)
assert not api('/auth')['initialized']
print('PASS: explicit purge removes application data; fresh reinstall awaits initialization',flush=True)
