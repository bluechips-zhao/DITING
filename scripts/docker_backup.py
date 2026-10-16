"""Encrypted, stopped-application backups for DITING Compose deployments.

Restore only creates a new project, empty database and new volumes. It does not
start the recovered application. No Docker socket is exposed to an application.
"""
import argparse
import base64
import hashlib
import json
import os
import posixpath
import re
import secrets
import struct
import subprocess
import tarfile
import tempfile
import time
import zipfile
from pathlib import Path, PurePosixPath

from cryptography.exceptions import InvalidTag
from cryptography.hazmat.primitives.ciphers.aead import AESGCM

MAGIC = b'DITDCK01'
CHUNK = 1024 * 1024
MAX_BYTES = 50 * 1024**3
VOLUMES = ('runtime', 'workspace', 'skills', 'toolhome')
PAYLOADS = ('database.dump', *(name + '.tar.gz' for name in VOLUMES))
DESTINATIONS = dict(zip(VOLUMES, ('runtime', 'data', 'skills', 'home')))


def require_passphrase():
    value = os.environ.get('DITING_BACKUP_PASSPHRASE', '')
    if len(value) < 16:
        raise ValueError('Set DITING_BACKUP_PASSPHRASE to at least 16 characters; never put it in command arguments')
    return value.encode('utf-8')


def read_exact(stream, length):
    value = stream.read(length)
    if len(value) != length:
        raise ValueError('Truncated backup')
    return value


def crypt_archive(source, target, passphrase, decrypt=False, max_bytes=MAX_BYTES):
    """Authenticated chunks, including an authenticated end marker."""
    with open(source, 'rb') as incoming, open(target, 'xb') as outgoing:
        if decrypt:
            if read_exact(incoming, 8) != MAGIC:
                raise ValueError('Not a DITING Docker backup')
            salt = read_exact(incoming, 16)
        else:
            salt = secrets.token_bytes(16)
            outgoing.write(MAGIC + salt)
        aes = AESGCM(hashlib.pbkdf2_hmac('sha256', passphrase, salt, 200000, 32))
        total, index = 0, 0
        while True:
            if decrypt:
                length = struct.unpack('<I', read_exact(incoming, 4))[0]
                if length > CHUNK:
                    raise ValueError('Invalid chunk size')
                nonce = read_exact(incoming, 12)
                data = read_exact(incoming, length + 16)
                try:
                    plain = aes.decrypt(nonce, data, MAGIC + struct.pack('<QI', index, length))
                except InvalidTag:
                    raise ValueError('Wrong passphrase or altered backup') from None
                if length == 0 and incoming.read(1):
                    raise ValueError('Trailing backup content')
                outgoing.write(plain)
            else:
                plain = incoming.read(CHUNK)
                length = len(plain)
                nonce = secrets.token_bytes(12)
                data = aes.encrypt(nonce, plain, MAGIC + struct.pack('<QI', index, length))
                outgoing.write(struct.pack('<I', length) + nonce + data)
            total += length
            if total > max_bytes:
                raise ValueError('Backup exceeds size limit')
            if length == 0:
                break
            index += 1
        outgoing.flush()
        os.fsync(outgoing.fileno())


def digest(path):
    with open(path, 'rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def validate_tar(path, max_bytes=MAX_BYTES):
    total, seen, files, parents, links, regular = 0, set(), set(), set(), {}, set()
    with tarfile.open(path, 'r:gz') as archive:
        for member in archive:
            name = member.name
            parsed = PurePosixPath(name)
            if parsed.is_absolute() or '..' in parsed.parts or '\\' in name or ':' in name:
                raise ValueError('Unsafe volume archive path')
            if not (member.isfile() or member.isdir() or member.issym() or member.islnk()):
                raise ValueError('Volume contains a special file')
            normalized = str(parsed)
            if normalized in seen:
                raise ValueError('Duplicate volume archive path')
            ancestors = {str(parent) for parent in parsed.parents}
            if ancestors & files or (not member.isdir() and normalized in parents):
                raise ValueError('Volume archive file conflicts with a directory path')
            seen.add(normalized)
            parents.update(ancestors)
            if not member.isdir():
                if normalized == '.':
                    raise ValueError('Volume root must be a directory')
                files.add(normalized)
            if member.issym() or member.islnk():
                target = member.linkname
                if not target or PurePosixPath(target).is_absolute() or '\\' in target or ':' in target:
                    raise ValueError('Unsafe volume archive link')
                raw_target = posixpath.join(str(parsed.parent), target) if member.issym() else target
                resolved = posixpath.normpath(raw_target)
                if resolved == '..' or resolved.startswith('../'):
                    raise ValueError('Volume archive link escapes its volume')
                if member.islnk():
                    if resolved not in regular:
                        raise ValueError('Hard link must reference an earlier regular file')
                    regular.add(normalized)
                links[normalized] = raw_target
            if member.isfile():
                regular.add(normalized)
            total += member.size
            if total > max_bytes:
                raise ValueError('Volume exceeds size limit')
    # Resolve links before '..': lexical normalization alone can hide an escape
    # when a target directory is itself a symlink to the volume root.
    for original, target in links.items():
        visited, pending, stack = {original}, list(PurePosixPath(target).parts), []
        while pending:
            part = pending.pop(0)
            if part == '..':
                if not stack:
                    raise ValueError('Volume archive link escapes its volume')
                stack.pop()
                continue
            stack.append(part)
            linked = '/'.join(stack)
            if linked in links:
                if linked in visited:
                    raise ValueError('Cyclic volume archive link')
                visited.add(linked)
                pending = list(PurePosixPath(links[linked]).parts) + pending
                stack = []
    return total


def verify_archive(path, stage, max_bytes=MAX_BYTES):
    """Verify every hash and volume before any Docker mutation."""
    with zipfile.ZipFile(path) as archive:
        names = archive.namelist()
        if len(names) != len(set(names)) or set(names) != {'manifest.json', *PAYLOADS}:
            raise ValueError('Unexpected backup inventory')
        if sum(entry.file_size for entry in archive.infolist()) > max_bytes or archive.getinfo('manifest.json').file_size > 65536:
            raise ValueError('Backup inventory exceeds size limit')
        manifest = json.loads(archive.read('manifest.json'))
        if not isinstance(manifest, dict) or manifest.get('format') != 1 or not isinstance(manifest.get('files'), dict) or set(manifest['files']) != set(PAYLOADS):
            raise ValueError('Invalid backup manifest')
        if not isinstance(manifest.get('image_id'), str) or not re.fullmatch(r'sha256:[0-9a-f]{64}', manifest['image_id']):
            raise ValueError('Invalid application image identity')
        if not isinstance(manifest.get('project'), str) or not re.fullmatch(r'[a-z0-9][a-z0-9_-]{0,62}', manifest['project']):
            raise ValueError('Invalid source project')
        unpacked = 0
        for name in PAYLOADS:
            target = stage / name
            with archive.open(name) as source, open(target, 'xb') as output:
                while chunk := source.read(CHUNK):
                    output.write(chunk)
            expected = manifest['files'][name]
            if not isinstance(expected, dict):
                raise ValueError('Invalid file manifest')
            if target.stat().st_size != expected.get('bytes') or digest(target) != expected.get('sha256'):
                raise ValueError('Backup checksum mismatch')
            unpacked += validate_tar(target, max_bytes) if name.endswith('.tar.gz') else target.stat().st_size
            if unpacked > max_bytes:
                raise ValueError('Expanded backup exceeds size limit')
        validate_runtime_keys(stage / 'runtime.tar.gz')
        return manifest


def validate_runtime_keys(path):
    keys = {}
    with tarfile.open(path, 'r:gz') as archive:
        for member in archive:
            name = str(PurePosixPath(member.name))
            if name in ('secrets.key', 'jwt.key'):
                if not member.isfile() or member.size != 32:
                    raise ValueError('Invalid runtime key')
                keys[name] = archive.extractfile(member).read()
    if set(keys) != {'secrets.key', 'jwt.key'}:
        raise ValueError('Backup requires both the credential and JWT keys')
    return keys['secrets.key']


def check_credential_key(key, rows):
    aes = AESGCM(key)
    for scope, sealed in rows:
        try:
            encoded = sealed.removeprefix('diting:v1:')
            ciphertext = base64.b64decode(encoded + '=' * (-len(encoded) % 4), validate=True)
            if not sealed.startswith('diting:v1:') or len(ciphertext) < 28:
                raise ValueError()
            aes.decrypt(ciphertext[:12], ciphertext[12:], scope.encode('utf-8'))
        except (ValueError, InvalidTag):
            raise ValueError('Credential key does not decrypt the database; restore the matching secrets.key') from None


def database_credentials(container):
    fields = [('llm_profiles', 'api_key', False), ('llm_profiles', 'proxy', False), ('llm_profiles', 'base_url', False), ('notification_channels', 'config', True), ('mcp_servers', 'args', True), ('mcp_servers', 'env', True), ('mcp_servers', 'url', False)]
    selects = ["SELECT '"+table+'.'+column+"' AS scope, "+(column+"->>'_diting_secret_v1'" if is_json else column)+" AS value FROM "+table for table, column, is_json in fields]
    selects.append("SELECT 'settings.'||key, value FROM settings WHERE key IN ('brave_search_api_key','tavily_search_api_key','global_proxy','web_search_proxy')")
    query = "SELECT COALESCE(json_agg(json_build_array(scope,value)), '[]'::json) FROM ("+' UNION ALL '.join(selects)+") q WHERE value LIKE 'diting:v1:%'"
    return json.loads(docker('exec', container, 'psql', '-U', 'diting', '-d', 'diting', '-X', '-A', '-t', '-v', 'ON_ERROR_STOP=1', '-c', query))


def command(args, *, incoming=None, output=None, timeout=300):
    result = subprocess.run(args, stdin=incoming, stdout=output or subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout)
    if result.returncode:
        # SQL errors can contain saved user records: do not echo stderr.
        raise RuntimeError('Docker operation failed: ' + ' '.join(args[:3]))
    return result.stdout.decode('utf-8').strip() if output is None else None


def docker(*args, **kwargs):
    return command(['docker', *args], **kwargs)


def inspect(container):
    return json.loads(docker('inspect', container))[0]


def compose(args, *operation):
    cli = ['docker', 'compose', '-p', args.project, '-f', str(args.compose_file)]
    if args.env_file:
        cli += ['--env-file', str(args.env_file)]
    return command([*cli, *operation])


def service(args, name):
    ids = compose(args, 'ps', '-a', '-q', name).splitlines()
    if len(ids) != 1:
        raise ValueError('Expected one existing ' + name + ' service')
    info = inspect(ids[0])
    labels = info['Config'].get('Labels', {})
    if labels.get('com.docker.compose.project') != args.project or labels.get('com.docker.compose.service') != name:
        raise ValueError('Container identity does not match selected project')
    return info


def volume_mounts(app):
    result = {}
    for name, subdir in DESTINATIONS.items():
        found = [m for m in app['Mounts'] if m['Destination'] == '/var/lib/diting/' + subdir]
        if len(found) != 1 or found[0]['Type'] != 'volume':
            raise ValueError('Expected a named persistent ' + name + ' volume')
        result[name] = found[0]['Name']
    if len(set(result.values())) != len(VOLUMES):
        raise ValueError('Runtime and workspace must use distinct volumes')
    return result


def validate_restore_layout(layout, project):
    """A new project name must not hide existing external or host-mounted data."""
    volumes = layout.get('volumes', {})
    if set(volumes) != {'pgdata', *VOLUMES}:
        raise ValueError('Restore requires the five standard project-owned volumes')
    for name, volume in volumes.items():
        if volume.get('name') != project+'_'+name or volume.get('external') or volume.get('driver_opts') or volume.get('driver', 'local') != 'local':
            raise ValueError('Restore refuses external, shared or host-backed volumes')
    services = layout.get('services', {})
    if set(services) != {'artex', 'postgres'}:
        raise ValueError('Restore requires only the application and PostgreSQL services')
    expected = {'postgres': {'/var/lib/postgresql/data': 'pgdata'}, 'artex': {'/var/lib/diting/'+subdir: name for name, subdir in DESTINATIONS.items()}}
    for service_name, destinations in expected.items():
        service_config = services[service_name]
        mounts = service_config.get('volumes', [])
        if len(mounts) != len(destinations) or service_config.get('volumes_from'):
            raise ValueError('Restore refuses extra or inherited mounts')
        found = {}
        for mount in mounts:
            if mount.get('type') != 'volume' or mount.get('target') in found or mount.get('volume', {}).get('subpath'):
                raise ValueError('Restore refuses bind mounts or volume subpaths')
            found[mount.get('target')] = mount.get('source')
        if found != destinations:
            raise ValueError('Restore volume layout does not match the standard deployment')


def backup(args, stage):
    if not args.ack_stopped:
        raise ValueError('Stop the application and use --ack-stopped for a consistent backup')
    app, postgres = service(args, 'artex'), service(args, 'postgres')
    if app['State']['Running'] or app['State'].get('Restarting'):
        raise ValueError('Application is running; stop artex before backup')
    if not postgres['State']['Running']:
        raise ValueError('PostgreSQL must be running for the logical dump')
    mounts = volume_mounts(app)
    with open(stage / 'database.dump', 'xb') as out:
        docker('exec', postgres['Id'], 'pg_dump', '-U', 'diting', '-d', 'diting', '--format=custom', '--no-owner', output=out)
    export_code = "import sys,tarfile; t=tarfile.open(fileobj=sys.stdout.buffer,mode='w|gz'); t.add('/source',arcname='.'); t.close()"
    for name, volume in mounts.items():
        with open(stage / (name + '.tar.gz'), 'xb') as out:
            docker('run', '--rm', '--network', 'none', '--read-only', '--user', '10001:10001', '--mount', 'type=volume,src='+volume+',dst=/source,readonly', '--entrypoint', 'python', app['Image'], '-c', export_code, output=out)
        validate_tar(stage / (name + '.tar.gz'))
    check_credential_key(validate_runtime_keys(stage/'runtime.tar.gz'), database_credentials(postgres['Id']))
    manifest = {'format': 1, 'project': args.project, 'image_id': app['Image'], 'created_utc': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()), 'files': {name: {'bytes': (stage/name).stat().st_size, 'sha256': digest(stage/name)} for name in PAYLOADS}}
    archive_path = stage / 'snapshot.zip'
    with zipfile.ZipFile(archive_path, 'x', compression=zipfile.ZIP_DEFLATED) as archive:
        archive.writestr('manifest.json', json.dumps(manifest))
        for name in PAYLOADS:
            archive.write(stage/name, name)
    if args.file.exists():
        raise ValueError('Backup output exists; choose a new path')
    args.file.parent.mkdir(parents=True, exist_ok=True)
    partial = args.file.with_name(args.file.name + '.partial-' + secrets.token_hex(8))
    try:
        crypt_archive(archive_path, partial, require_passphrase())
        # Exclusive target creation avoids replacing a concurrently created file.
        os.link(partial, args.file)
    finally:
        partial.unlink(missing_ok=True)
    return {'mode': 'backup', 'encrypted': True, 'sha256': digest(args.file), 'files': len(PAYLOADS)}


def restore(args, stage, manifest):
    if not args.ack_stopped:
        raise ValueError('Restore requires --ack-stopped and a new project')
    if args.project == manifest['project']:
        raise ValueError('Restore requires a different, new project; source deployment is never overwritten')
    if docker('ps', '-a', '-q', '--filter', 'label=com.docker.compose.project='+args.project):
        raise ValueError('Destination project already has containers')
    existing = set(docker('volume', 'ls', '--format', '{{.Name}}').splitlines())
    if docker('volume', 'ls', '-q', '--filter', 'label=com.docker.compose.project='+args.project) or any(args.project+'_'+v in existing for v in ('pgdata', *VOLUMES)):
        raise ValueError('Destination project already has data volumes')
    validate_restore_layout(json.loads(compose(args, 'config', '--format', 'json')), args.project)
    # The exact original image must be loaded locally before recovery.
    docker('image', 'inspect', manifest['image_id'])
    previous_image = os.environ.get('DITING_IMAGE')
    os.environ['DITING_IMAGE'] = manifest['image_id']
    try:
        compose(args, 'up', '-d', 'postgres')
        until = time.monotonic() + 90
        while True:
            postgres = service(args, 'postgres')
            if postgres['State'].get('Health', {}).get('Status') == 'healthy':
                break
            if time.monotonic() > until:
                raise ValueError('New PostgreSQL did not become healthy; destination retained for inspection')
            time.sleep(.5)
        count = docker('exec', postgres['Id'], 'psql', '-U', 'diting', '-d', 'diting', '-X', '-A', '-t', '-v', 'ON_ERROR_STOP=1', '-c', "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname NOT LIKE 'pg_%' AND n.nspname <> 'information_schema' AND c.relkind IN ('r','p','v','m','S','f')")
        if count != '0':
            raise ValueError('Destination database is not empty')
        compose(args, 'create', '--no-build', 'artex')
        app = service(args, 'artex')
        if app['State']['Running']:
            raise ValueError('Destination application unexpectedly running')
        mounts = volume_mounts(app)
        empty_code = "from pathlib import Path; import sys; sys.exit(1 if any(Path('/destination').iterdir()) else 0)"
        for volume in mounts.values():
            docker('run', '--rm', '--network', 'none', '--read-only', '--mount', 'type=volume,src='+volume+',dst=/destination,readonly', '--entrypoint', 'python', manifest['image_id'], '-c', empty_code)
        with open(stage/'database.dump', 'rb') as incoming:
            docker('exec', '-i', postgres['Id'], 'pg_restore', '-U', 'diting', '-d', 'diting', '--no-owner', '--no-privileges', '--single-transaction', '--exit-on-error', incoming=incoming)
        check_credential_key(validate_runtime_keys(stage/'runtime.tar.gz'), database_credentials(postgres['Id']))
        extract_code = "import sys,tarfile,os; t=tarfile.open(fileobj=sys.stdin.buffer,mode='r|gz'); t.extractall('/destination',filter='data'); t.close();\nfor p,ds,fs in os.walk('/destination'):\n os.chown(p,10001,10001,follow_symlinks=False)\n for n in ds+fs: os.chown(os.path.join(p,n),10001,10001,follow_symlinks=False)"
        for name, volume in mounts.items():
            with open(stage/(name+'.tar.gz'), 'rb') as incoming:
                docker('run', '--rm', '-i', '--network', 'none', '--read-only', '--user', '0:0', '--mount', 'type=volume,src='+volume+',dst=/destination', '--entrypoint', 'python', manifest['image_id'], '-c', extract_code, incoming=incoming)
        return {'mode': 'restore', 'project': args.project, 'image_id': manifest['image_id'], 'application': 'stopped', 'source_untouched': True}
    finally:
        if previous_image is None:
            os.environ.pop('DITING_IMAGE', None)
        else:
            os.environ['DITING_IMAGE'] = previous_image


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=('backup', 'preview', 'restore'))
    parser.add_argument('--file', type=Path, required=True)
    parser.add_argument('--project', default='diting')
    parser.add_argument('--compose-file', type=Path, default=Path(__file__).resolve().parents[1]/'docker-compose.yml')
    parser.add_argument('--env-file', type=Path)
    parser.add_argument('--ack-stopped', action='store_true')
    args = parser.parse_args()
    if not re.fullmatch(r'[a-z0-9][a-z0-9_-]{0,62}', args.project):
        parser.error('Use a lower-case Compose project name')
    args.file = args.file.resolve()
    require_passphrase()
    with tempfile.TemporaryDirectory(prefix='diting-docker-backup-') as folder:
        stage = Path(folder)
        if args.mode == 'backup':
            result = backup(args, stage)
        else:
            crypt_archive(args.file, stage/'verified.zip', require_passphrase(), decrypt=True)
            manifest = verify_archive(stage/'verified.zip', stage)
            result = {'mode': 'preview', 'verified': True, 'files': manifest['files'], 'image_id': manifest['image_id'], 'project': manifest['project']}
            if args.mode == 'restore':
                result = restore(args, stage, manifest)
        print(json.dumps(result, ensure_ascii=False))


if __name__ == '__main__':
    try:
        main()
    except (ValueError, RuntimeError, OSError, subprocess.TimeoutExpired, zipfile.BadZipFile, tarfile.TarError) as error:
        raise SystemExit(str(error)) from None
