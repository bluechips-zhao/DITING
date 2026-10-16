"""Prepare persistent runtime paths and exec the supervised application."""
import os
import shutil
import sys
from pathlib import Path
from urllib.parse import quote

home = Path(os.environ.get('ARTEX_HOME', '/var/lib/diting/runtime'))
skills = Path(os.environ.get('ARTEX_SKILL_DIR', '/var/lib/diting/skills'))
for directory in (home, skills, Path('/var/lib/diting/data'), Path(os.environ['HOME'])):
    directory.mkdir(mode=0o700, parents=True, exist_ok=True)
# Initialize an empty skills volume once. Rebuilds preserve all user edits.
if not any(skills.iterdir()):
    shutil.copytree('/opt/diting/skills', skills, dirs_exist_ok=True)
password = os.environ.pop('DITING_DATABASE_PASSWORD', '')
if not os.environ.get('ARTEX_PG_DSN'):
    if not password:
        raise SystemExit('Database password is required')
    os.environ['ARTEX_PG_DSN'] = 'postgres://diting:' + quote(password, safe='') + '@postgres:5432/diting?sslmode=disable'
os.umask(0o077)
args = sys.argv[1:] or ['-addr', ':8787', '-proxy', ':8788', '-data', '/var/lib/diting/data']
os.execv('/app/start.sh', ['/app/start.sh', *args])
