#!/usr/bin/env python3
"""Root-only single-file CAS deployment. Restart is intentionally separate."""
import hashlib,json,os,pathlib,shutil,stat,subprocess,sys,time
TARGET=pathlib.Path('/var/lib/openclaw-qq-capabilities-20261023/extensions/openclaw-onebot/dist/src/gateway.js')
BASE='0b8db1326e469f867c1c238b9bfb0fb7928dd3e4e600768e70c366a00c95ef16'
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
assert os.geteuid()==0
assert len(sys.argv)==2
candidate=pathlib.Path(sys.argv[1]).resolve()
assert sha(TARGET)==BASE,'live source drift'
s=TARGET.stat()
assert (s.st_uid,s.st_gid,stat.S_IMODE(s.st_mode))==(0,0,0o644)
assert candidate.read_text().count('if (isSub2APIPrivateCommand(event)) return;')==1
subprocess.run(['/var/lib/openclaw-boundary-release-20261021/code/bin/node','--check',str(candidate)],check=True)
backup=pathlib.Path('/var/backups/sub2api-qq')/time.strftime('%Y%m%dT%H%M%SZ',time.gmtime())
backup.mkdir(parents=True,mode=0o700)
shutil.copy2(TARGET,backup/'gateway.js.before')
metadata={'path':str(TARGET),'uid':s.st_uid,'gid':s.st_gid,'mode':stat.S_IMODE(s.st_mode),'sha_before':BASE,'sha_after':sha(candidate)}
(backup/'metadata.json').write_text(json.dumps(metadata,indent=2)+'\n')
assert sha(TARGET)==BASE,'live source drift before swap'
tmp=TARGET.with_name('gateway.js.sub2api-new')
with tmp.open('xb') as f:f.write(candidate.read_bytes());f.flush();os.fsync(f.fileno())
os.chown(tmp,s.st_uid,s.st_gid);os.chmod(tmp,stat.S_IMODE(s.st_mode))
assert sha(TARGET)==BASE,'live source drift before swap'
os.replace(tmp,TARGET)
assert sha(TARGET)==metadata['sha_after']
print(json.dumps({'backup':str(backup),'sha_after':sha(TARGET),'restart_performed':False}))
