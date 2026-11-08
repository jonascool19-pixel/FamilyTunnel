#!/usr/bin/env bash
# Called by the installer on Oracle. No API keys are collected or embedded.
set -Eeuo pipefail
umask 077
state=${1:-/var/lib/familytunnel}
helper=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)/oci-nsg.py
[[ $EUID == 0 ]] || { echo 'Root-Rechte erforderlich.' >&2; exit 1; }
if [[ ${FT_SKIP_OCI_AUTOMATION:-0} == 1 ]]; then
 echo 'Oracle-Automation ausdrücklich übersprungen; Cloud-Regeln sind nicht automatisch bestätigt.'
 exit 0
fi
tmp=$(mktemp -d /var/tmp/familytunnel-oracle.XXXXXX)
trap 'rm -rf -- "$tmp"' EXIT
if ! python3 - "$tmp/instance.json" <<'PY'
import json,sys,urllib.request
try:
    r=urllib.request.Request('http://169.254.169.254/opc/v2/instance/',headers={'Authorization':'Bearer Oracle'})
    with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(r,timeout=3) as response:
        value=json.loads(response.read(65536))
    out={key:value[key] for key in ('id','compartmentId','region')}
    with open(sys.argv[1],'w') as f: json.dump(out,f)
except Exception:
    raise SystemExit(1)
PY
then
 echo 'Oracle-Metadaten nicht erreichbar. Eine automatische Cloud-Freigabe ist nicht bestätigt.' >&2
 echo 'Auf einer Oracle-VM den Metadatenzugriff prüfen. Für andere VPS gibt es FT_SKIP_OCI_AUTOMATION=1.' >&2
 exit 1
fi
if [[ ! -x /usr/local/bin/oci && ! -x /usr/bin/oci ]]; then
 echo 'Oracle-Verbindungswerkzeug wird automatisch eingerichtet …'
 apt-get install -y python3-venv
 python3 -m venv /opt/familytunnel/oci-cli
 /opt/familytunnel/oci-cli/bin/python -m pip --disable-pip-version-check --timeout 20 --retries 2 install 'oci-cli==3.96.0'
 if [[ -e /usr/local/bin/oci || -L /usr/local/bin/oci ]]; then
  [[ $(readlink -f /usr/local/bin/oci) == /opt/familytunnel/oci-cli/bin/oci ]] || { echo 'Vorhandener OCI-Pfad wird nicht überschrieben.' >&2; exit 1; }
 else
  ln -s /opt/familytunnel/oci-cli/bin/oci /usr/local/bin/oci
 fi
fi
if python3 "$helper" --auto-setup --state-dir "$state"; then
 echo 'Oracle-Anbindung und Cloud-Freigaben automatisch bestätigt.'
 exit 0
fi
echo >&2
echo 'Die automatische Oracle-Anbindung ist noch nicht vollständig. Häufig fehlt die einmalige Kontofreigabe.' >&2
echo 'Es muss keine NSG manuell gesucht werden. Ein Oracle-Kontoadministrator kann die Freigabe in Cloud Shell ausführen:' >&2
python3 - "$tmp/instance.json" <<'PY'
import json,re,shlex,sys
d=json.load(open(sys.argv[1]))
if not all(re.fullmatch(r'[A-Za-z0-9._-]{1,512}',str(v)) for v in d.values()):
    raise SystemExit('Ungültige Oracle-Metadaten')
args=['--instance',d['id'],'--compartment',d['compartmentId'],'--region',d['region']]
print("curl -fsSL --connect-timeout 10 --max-time 60 https://raw.githubusercontent.com/jonascool19-pixel/FamilyTunnel/main/scripts/oracle-permissions.py | python3 - "+shlex.join(args))
PY
echo 'Dabei werden ausschließlich begrenzte Netzwerkrechte für genau diese VM eingerichtet.' >&2
echo 'Danach den ursprünglichen Installationsbefehl erneut ausführen. Vorhandene Schlüssel und Daten bleiben erhalten.' >&2
echo 'Installation noch nicht vollständig: Internetfreigaben werden nicht als erfolgreich ausgegeben.' >&2
exit 1
