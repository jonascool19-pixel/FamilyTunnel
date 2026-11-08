import hashlib
import importlib.util
import io
import pathlib
import tarfile
import tempfile
import unittest

spec=importlib.util.spec_from_file_location('audit',pathlib.Path(__file__).with_name('package-audit.py'))
audit=importlib.util.module_from_spec(spec);spec.loader.exec_module(audit)

class PackageAudit(unittest.TestCase):
    def files(self):
        files={name:b'' for name in audit.REQUIRED}
        binaries={name:b'\x7fELF-test-'+name.encode() for name in audit.BINARIES}
        files.update({'dist/'+k:v for k,v in binaries.items()})
        files['dist/SHA256SUMS']=''.join(hashlib.sha256(v).hexdigest()+'  '+k+'\n' for k,v in binaries.items()).encode()
        return files

    def check(self,files,link=False):
        with tempfile.TemporaryDirectory() as d:
            path=pathlib.Path(d)/'install.tar.gz'
            with tarfile.open(path,'w:gz') as archive:
                for name,body in files.items():
                    item=tarfile.TarInfo('familytunnel/'+name);item.size=len(body)
                    archive.addfile(item,io.BytesIO(body))
                if link:
                    item=tarfile.TarInfo('familytunnel/docs/link.md');item.type=tarfile.SYMTYPE;item.linkname='/etc/shadow';archive.addfile(item)
            return audit.audit(path)

    def test_runtime_only_archive(self):self.assertEqual(self.check(self.files()),len(audit.REQUIRED))

    def test_demo_state_and_traversal_are_rejected(self):
        for extra in ('state.db','pairing.json','scripts/preview-web.cjs','tests/ui/preview.cjs','docs/private.key','../outside'):
            with self.subTest(extra=extra):
                files=self.files();files[extra]=b'not allowed'
                with self.assertRaises(ValueError):self.check(files)
        with self.assertRaises(ValueError):self.check(self.files(),link=True)

    def test_key_payload_and_tampered_binary_are_rejected(self):
        files=self.files();files['scripts/manage.sh']=b'-----BEGIN PRIVATE KEY-----\n'+b'A'*64
        with self.assertRaises(ValueError):self.check(files)
        files=self.files();files['dist/familytunnel-linux-amd64']+=b'tampered'
        with self.assertRaises(ValueError):self.check(files)

unittest.main()
