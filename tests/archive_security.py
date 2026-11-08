import importlib.util
import io
import pathlib
import subprocess
import sys
import tarfile
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('safe_archive', pathlib.Path(__file__).resolve().parents[1] / 'scripts/archive.py')
archive = importlib.util.module_from_spec(spec)
spec.loader.exec_module(archive)


class ArchiveSecurity(unittest.TestCase):
    def extract(self, names, kind='update', special=None):
        with tempfile.TemporaryDirectory() as folder:
            root = pathlib.Path(folder)
            path = root / 'test.tar.gz'
            with tarfile.open(path, 'w:gz') as tar:
                for name in names:
                    item = tarfile.TarInfo(name)
                    item.size = 2
                    if special:
                        special(item)
                    tar.addfile(item, io.BytesIO(b'{}') if item.isfile() else None)
            target = root / 'out'
            target.mkdir()
            archive.extract(path, target, kind)
            return sorted(p.relative_to(target).as_posix() for p in target.rglob('*') if p.is_file())

    def test_plain_release(self):
        self.assertEqual(self.extract(['release.json']), ['release.json'])

    def test_paths_links_devices_and_duplicates(self):
        for name in ['../outside', '/absolute', 'dir/../../escape', 'bad\\name', 'bad\nname']:
            with self.subTest(name=name), self.assertRaises(ValueError):
                self.extract([name])
        for kind in [tarfile.SYMTYPE, tarfile.LNKTYPE, tarfile.CHRTYPE, tarfile.FIFOTYPE]:
            def special(item):
                item.type = kind
                item.linkname = '/etc/shadow'
            with self.subTest(kind=kind), self.assertRaises(ValueError):
                self.extract(['release.json'], special=special)
        with self.assertRaises(ValueError):
            self.extract(['release.json', 'release.json'])

    def test_archive_scope_and_modes(self):
        self.assertEqual(self.extract(['./state.db', './oci/config'], 'backup'), ['oci/config', 'state.db'])
        self.assertEqual(self.extract(['familytunnel/install.sh'], 'install'), ['familytunnel/install.sh'])
        for kind, name in [('update','state.db'), ('backup','etc/shadow'), ('install','install.sh')]:
            with self.assertRaises(ValueError):
                self.extract([name], kind)
        with self.assertRaises(ValueError):
            self.extract(['release.json'], special=lambda item: setattr(item, 'mode', 0o4755))

    def test_validation_precedes_any_write(self):
        with tempfile.TemporaryDirectory() as folder:
            root = pathlib.Path(folder)
            with tarfile.open(root/'bad.tar.gz','w:gz') as tar:
                for name in ['release.json','../escape']:
                    item=tarfile.TarInfo(name);item.size=2;tar.addfile(item,io.BytesIO(b'{}'))
            with self.assertRaises(ValueError):
                archive.extract(root/'bad.tar.gz', root/'out','update')
            self.assertFalse((root/'out').exists())

    def test_bootstrap_checks_archive_before_root_extraction(self):
        bootstrap=(pathlib.Path(__file__).resolve().parents[1]/'bootstrap.sh').read_text()
        code=bootstrap.split("<<'PYSAFE'\n",1)[1].split('\nPYSAFE',1)[0]
        with tempfile.TemporaryDirectory() as folder:
            path=pathlib.Path(folder)/'bundle.tar.gz'
            for unsafe in (False, True):
                with tarfile.open(path,'w:gz') as tar:
                    item=tarfile.TarInfo('familytunnel/install.sh');item.size=2
                    tar.addfile(item,io.BytesIO(b'{}'))
                    if unsafe:
                        link=tarfile.TarInfo('familytunnel/link');link.type=tarfile.SYMTYPE;link.linkname='/etc';tar.addfile(link)
                result=subprocess.run([sys.executable,'-c',code,str(path)],capture_output=True)
                self.assertEqual(result.returncode==0,not unsafe,result.stderr)


if __name__ == '__main__':
    unittest.main()
