import base64
import copy
import io
import json
import os
import secrets
import tarfile
import tempfile
import unittest
import zipfile
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

import docker_backup as backup


class BackupTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)

    def tar(self, name, entries):
        path = self.root/name
        with tarfile.open(path, 'w:gz') as archive:
            for entry, value in entries:
                member = tarfile.TarInfo(entry)
                if isinstance(value, bytes):
                    member.size = len(value)
                    archive.addfile(member, io.BytesIO(value))
                else:
                    member.type = value[0]
                    member.linkname = value[1] if len(value) > 1 else '/outside'
                    archive.addfile(member)
        return path

    def test_authenticated_roundtrip_multiple_chunks(self):
        source = self.root/'source'
        source.write_bytes(secrets.token_bytes(backup.CHUNK+73))
        sealed, restored = self.root/'sealed', self.root/'restored'
        backup.crypt_archive(source, sealed, b'fixture-password-at-least-16')
        self.assertNotIn(source.read_bytes()[:32], sealed.read_bytes())
        backup.crypt_archive(sealed, restored, b'fixture-password-at-least-16', decrypt=True)
        self.assertEqual(source.read_bytes(), restored.read_bytes())

    def test_bad_passphrase_tampering_truncation_and_trailing_bytes(self):
        source, sealed = self.root/'source', self.root/'sealed'
        source.write_bytes(b'fixture sensitive value')
        backup.crypt_archive(source, sealed, b'fixture-password-at-least-16')
        original = sealed.read_bytes()
        samples = [original[:-1], original[:-32], original+b'x', original[:40]+bytes([original[40]^1])+original[41:]]
        for i, value in enumerate(samples):
            with self.subTest(i=i):
                altered = self.root/('altered'+str(i)); altered.write_bytes(value)
                with self.assertRaises(ValueError):
                    backup.crypt_archive(altered, self.root/('result'+str(i)), b'fixture-password-at-least-16', decrypt=True)
        with self.assertRaises(ValueError):
            backup.crypt_archive(sealed, self.root/'wrong', b'wrong-password-at-least-16', decrypt=True)

    def test_output_is_never_overwritten_and_size_is_bounded(self):
        source, target = self.root/'source', self.root/'target'
        source.write_bytes(b'hello'); target.write_bytes(b'keep')
        with self.assertRaises(FileExistsError):
            backup.crypt_archive(source, target, b'fixture-password-at-least-16')
        self.assertEqual(target.read_bytes(), b'keep')
        with self.assertRaises(ValueError):
            backup.crypt_archive(source, self.root/'small', b'fixture-password-at-least-16', max_bytes=4)

    def test_unsafe_tar_names_links_special_files_and_duplicates(self):
        cases = [('parent', [('../escape', b'x')]), ('absolute', [('/escape', b'x')]), ('backslash', [('a\\b', b'x')]), ('link', [('link', (tarfile.SYMTYPE,))]), ('hardlink', [('link', (tarfile.LNKTYPE,))]), ('fifo', [('fifo', (tarfile.FIFOTYPE,))]), ('duplicate', [('same', b'a'), ('./same', b'b')])]
        for name, entries in cases:
            with self.subTest(name=name):
                path = self.tar(name+'.tar.gz', entries)
                with self.assertRaises(ValueError): backup.validate_tar(path)

    def test_runtime_keys_and_credential_key_matching(self):
        key = secrets.token_bytes(32)
        runtime = self.tar('runtime.tar.gz', [('secrets.key', key), ('jwt.key', secrets.token_bytes(32))])
        self.assertEqual(backup.validate_runtime_keys(runtime), key)
        nonce = secrets.token_bytes(12)
        scope = 'llm_profiles.api_key'
        encoded = nonce+backup.AESGCM(key).encrypt(nonce, b'fixture-private', scope.encode())
        sealed = 'diting:v1:'+base64.b64encode(encoded).decode().rstrip('=')
        backup.check_credential_key(key, [(scope, sealed)])
        for wrong_key, wrong_scope, wrong_value in [(b'x'*32, scope, sealed), (key, 'mcp_servers.url', sealed), (key, scope, 'diting:v1:broken')]:
            with self.assertRaises(ValueError): backup.check_credential_key(wrong_key, [(wrong_scope, wrong_value)])
        absent = self.tar('absent.tar.gz', [('jwt.key', b'x'*32)])
        with self.assertRaises(ValueError): backup.validate_runtime_keys(absent)

    def test_tar_file_parent_conflicts_in_both_orders(self):
        for i, entries in enumerate([[('parent', b'x'), ('parent/child', b'y')], [('parent/child', b'y'), ('parent', b'x')], [('.', b'x')]]):
            with self.subTest(i=i):
                with self.assertRaises(ValueError):
                    backup.validate_tar(self.tar('conflict'+str(i)+'.tar.gz', entries))

    def test_safe_links_roundtrip_and_unsafe_chains_rejected(self):
        safe = self.tar('safe.tar.gz', [('file', b'preserved'), ('alias', (tarfile.SYMTYPE, 'file')), ('hard', (tarfile.LNKTYPE, 'file')), ('nested/alias', (tarfile.SYMTYPE, '../file'))])
        backup.validate_tar(safe)
        destination = self.root/'links'; destination.mkdir()
        with tarfile.open(safe) as archive: archive.extractall(destination, filter='data')
        # Windows tarfile can copy link contents when symlink creation lacks privilege.
        if os.name != 'nt': self.assertTrue((destination/'alias').is_symlink())
        self.assertEqual((destination/'alias').read_bytes(), b'preserved')
        self.assertEqual((destination/'nested/alias').read_bytes(), b'preserved')
        self.assertEqual((destination/'file').stat().st_ino, (destination/'hard').stat().st_ino)
        invalid = [[('link', (tarfile.SYMTYPE, '../outside'))], [('a', (tarfile.SYMTYPE, 'b')), ('b', (tarfile.SYMTYPE, 'a'))], [('dir', (tarfile.SYMTYPE, 'actual')), ('dir/file', b'bad')], [('a', (tarfile.SYMTYPE, 'a/child'))], [('hard', (tarfile.LNKTYPE, '../outside'))]]
        for i, entries in enumerate(invalid):
            with self.subTest(i=i):
                with self.assertRaises(ValueError): backup.validate_tar(self.tar('links-bad'+str(i)+'.tar.gz', entries))

    def test_manifest_identity_types_rejected_before_extraction(self):
        path = self.root/'invalid.zip'
        for i, changes in enumerate([{'image_id': None}, {'image_id': []}, {'project': 12}, {'project': {}}]):
            manifest = {'format': 1, 'project': 'fixture', 'image_id': 'sha256:'+'a'*64, 'files': {name: {} for name in backup.PAYLOADS}, **changes}
            with zipfile.ZipFile(path, 'w') as archive:
                archive.writestr('manifest.json', json.dumps(manifest))
                for name in backup.PAYLOADS: archive.writestr(name, b'')
            stage = self.root/('invalid'+str(i)); stage.mkdir()
            with self.subTest(i=i):
                with self.assertRaises(ValueError): backup.verify_archive(path, stage)
                self.assertEqual(list(stage.iterdir()), [])

    def test_hardlinks_must_have_a_prior_regular_target(self):
        invalid = [[('hard', (tarfile.LNKTYPE, 'absent'))], [('hard', (tarfile.LNKTYPE, 'file')), ('file', b'x')], [('alias', (tarfile.SYMTYPE, 'file')), ('hard', (tarfile.LNKTYPE, 'alias')), ('file', b'x')]]
        for i, entries in enumerate(invalid):
            with self.subTest(i=i):
                with self.assertRaises(ValueError): backup.validate_tar(self.tar('hard-bad'+str(i)+'.tar.gz', entries))
        backup.validate_tar(self.tar('hard-chain.tar.gz', [('file', b'x'), ('first', (tarfile.LNKTYPE, 'file')), ('second', (tarfile.LNKTYPE, 'first'))]))

    def test_symlink_parent_traversal_uses_resolved_directories(self):
        archive = self.tar('escape-chain.tar.gz', [('root-alias', (tarfile.SYMTYPE, '.')), ('escape', (tarfile.SYMTYPE, 'root-alias/../outside'))])
        with self.assertRaises(ValueError): backup.validate_tar(archive)
        safe = self.tar('safe-parent.tar.gz', [('nested', (tarfile.DIRTYPE,)), ('alias', (tarfile.SYMTYPE, 'nested')), ('file', b'x'), ('safe', (tarfile.SYMTYPE, 'alias/../file'))])
        backup.validate_tar(safe)

    def archive(self, extra=None, wrong_hash=False):
        files = {'database.dump': b'fixture dump'}
        for name in backup.VOLUMES:
            entries = [('fixture', b'preserved')]
            if name == 'runtime': entries += [('jwt.key', b'j'*32), ('secrets.key', b's'*32)]
            files[name+'.tar.gz'] = self.tar(name+'.tar.gz', entries).read_bytes()
        manifest = {'format': 1, 'project': 'fixture-source', 'image_id': 'sha256:'+'a'*64, 'files': {n: {'bytes': len(v), 'sha256': backup.hashlib.sha256(v).hexdigest()} for n,v in files.items()}}
        if wrong_hash: manifest['files']['database.dump']['sha256'] = '0'*64
        path = self.root/'archive.zip'
        with zipfile.ZipFile(path, 'w') as archive:
            archive.writestr('manifest.json', json.dumps(manifest))
            for n,v in files.items(): archive.writestr(n,v)
            if extra: archive.writestr(*extra)
        return path

    def test_verified_inventory_and_runtime_keys(self):
        archive = self.archive()
        stage = self.root/'verify'; stage.mkdir()
        manifest = backup.verify_archive(archive, stage)
        self.assertEqual(manifest['project'], 'fixture-source')
        self.assertEqual((stage/'database.dump').read_bytes(), b'fixture dump')

    def test_unexpected_zip_path_and_hash_mismatch_rejected(self):
        for i, options in enumerate([{'extra': ('../escape', b'bad')}, {'wrong_hash': True}]):
            archive = self.archive(**options)
            stage = self.root/('verify'+str(i)); stage.mkdir()
            with self.assertRaises(ValueError): backup.verify_archive(archive, stage)

    def test_restore_refuses_source_or_existing_destination_before_mutations(self):
        args = SimpleNamespace(ack_stopped=True, project='fixture-source')
        with patch.object(backup, 'docker') as docker, patch.object(backup, 'compose') as compose:
            with self.assertRaises(ValueError): backup.restore(args, self.root, {'project':'fixture-source'})
            docker.assert_not_called(); compose.assert_not_called()
        args.project = 'fixture-new'
        for responses in [['existing-container'], ['', 'fixture-new_runtime', ''], ['', '', 'existing-labelled-volume']]:
            with patch.object(backup, 'docker', side_effect=responses), patch.object(backup, 'compose') as compose:
                with self.assertRaises(ValueError): backup.restore(args, self.root, {'project':'fixture-source'})
                compose.assert_not_called()

    def test_restore_rejects_external_and_host_backed_compose_layouts(self):
        project = 'fixture-new'
        layout = {'volumes': {name: {'name': project+'_'+name} for name in ('pgdata', *backup.VOLUMES)}, 'services': {'postgres': {'volumes': [{'type':'volume','source':'pgdata','target':'/var/lib/postgresql/data'}]}, 'artex': {'volumes':[{'type':'volume','source':name,'target':'/var/lib/diting/'+subdir} for name,subdir in backup.DESTINATIONS.items()]}}}
        backup.validate_restore_layout(layout, project)
        variations = []
        changed = copy.deepcopy(layout); changed['volumes']['pgdata']['external'] = True; variations.append(changed)
        changed = copy.deepcopy(layout); changed['volumes']['pgdata']['name'] = 'original_database'; variations.append(changed)
        changed = copy.deepcopy(layout); changed['volumes']['workspace']['driver_opts'] = {'type':'none','o':'bind','device':'/original'}; variations.append(changed)
        changed = copy.deepcopy(layout); changed['services']['artex']['volumes'][0]['type'] = 'bind'; variations.append(changed)
        changed = copy.deepcopy(layout); changed['services']['artex']['volumes_from'] = ['original']; variations.append(changed)
        changed = copy.deepcopy(layout); changed['services']['other'] = {}; variations.append(changed)
        for i, changed in enumerate(variations):
            with self.subTest(i=i):
                with self.assertRaises(ValueError): backup.validate_restore_layout(changed, project)


if __name__ == '__main__':
    unittest.main()
