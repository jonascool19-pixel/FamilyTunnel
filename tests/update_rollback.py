"""Run the real update function against an isolated directory and fake services."""
import hashlib
import io
import os
import pathlib
import shlex
import subprocess
import tarfile
import tempfile

repo = pathlib.Path(__file__).resolve().parents[1]
source = (repo/'scripts/manage.sh').read_text()
function = source[source.index('update() {'):source.index('\nuninstall() {')]
with tempfile.TemporaryDirectory(prefix='ft-rollback-') as folder:
    root = pathlib.Path(folder)
    for failure in ('restart', 'health'):
        test = root/failure
        test.mkdir()
        app = test/'app'
        (app/'old').mkdir(parents=True)
        (app/'current').symlink_to(app/'old', target_is_directory=True)
        release = test/'release.tar.gz'
        with tarfile.open(release,'w:gz') as tar:
            for name, data, mode in [('release.json',b'{"version":"0.1.0"}',0o600),('familytunnel-linux-amd64',b'#!/bin/sh\nexit 0\n',0o700)]:
                item=tarfile.TarInfo(name);item.size=len(data);item.mode=mode;tar.addfile(item,io.BytesIO(data))
        checksum=test/'checksum'
        checksum.write_text(hashlib.sha256(release.read_bytes()).hexdigest()+'  release.tar.gz\n')
        changed=function.replace('/opt/familytunnel',str(app)).replace('/usr/local/lib/familytunnel/archive.py',str(repo/'scripts/archive.py'))
        script = 'set -Eeuo pipefail\numask 077\n'+changed+f'''
backup() {{ printf '%s\\n' {shlex.quote(str(test/'backup'))}; }}
restore() {{ printf restored > {shlex.quote(str(test/'restored'))}; }}
sleep() {{ :; }}
systemctl() {{
 if [[ "$1" == restart ]]; then [[ {failure} != restart ]]; else return 1; fi
}}
dpkg() {{ printf amd64; }}
curl() {{
 local src={shlex.quote(str(release))} arg dest=''
 for arg in "$@"; do [[ "$arg" == *.sha256 ]] && src={shlex.quote(str(checksum))}; done
 while (( $# )); do if [[ "$1" == -o ]];then dest=$2;break;fi;shift;done
 cp "$src" "$dest"
}}
update https://example.net/release.tar.gz
'''
        result=subprocess.run(['bash','-c',script],capture_output=True,text=True,timeout=20)
        assert result.returncode != 0, result.stdout+result.stderr
        assert (app/'current').resolve()==app/'old', result.stdout+result.stderr
        assert (test/'restored').read_text()=='restored', result.stdout+result.stderr
        assert 'zurückgerollt' in result.stderr, result.stderr
        print('Real update rollback passed after',failure,'failure; no system service changed.')

    # Exercise the real backup function and its EXIT cleanup after local variables
    # have left scope. This used to leave the temporary secret copies behind.
    state=root/'state';state.mkdir()
    import sqlite3
    with sqlite3.connect(state/'state.db') as db:
        db.execute('CREATE TABLE example(value TEXT)')
    (state/'config.json').write_text('{"role":"client"}')
    (state/'ft0.conf').write_text('test fixture, no real key')
    backups=root/'backups';backups.mkdir()
    function=source[source.index('backup() {'):source.index('\nrestore() {')]
    function=function.replace('/etc/wireguard/ft0.conf',str(state/'ft0.conf')).replace('/var/tmp/familytunnel-backup.',str(root/'pending-'))
    script='set -Eeuo pipefail\numask 077\nSTATE='+shlex.quote(str(state))+'\nBACKUPS='+shlex.quote(str(backups))+'\n'+function+'\nbackup\n'
    result=subprocess.run(['bash','-c',script],capture_output=True,text=True,timeout=20)
    assert result.returncode==0,result.stdout+result.stderr
    assert not list(root.glob('pending-*')), 'Temporary backup copies retained'
    assert list(backups.glob('*.tar.gz')), 'Backup not created'
    print('Real backup and temporary secret cleanup passed in isolated directory.')
