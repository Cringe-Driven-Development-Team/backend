#!/usr/bin/env python3
"""Читает на VPS2 запущенные backend-сервисы и версии их схем, ничего не изменяя.

Запускается через Ansible (scripts/release/reconcile.yml) после прерванной операции.
Аргумент — каталог выкладки микросервисов infra (operation.json, compose.yml, deploy.lock).
Выводит JSON: для каждого сервиса running, image_ref, image_id, revision, source и
schema_version; отсутствующие сведения не заполняются.
"""
import fcntl
import json
from pathlib import Path
import re
import subprocess
import sys

SERVICES = ('identity', 'workspace', 'environments', 'onlineconf')


def run(*argv, input_data=None):
    """Выполняет команду и возвращает stdout; stderr не выводится, чтобы не раскрыть окружение."""
    return subprocess.run(argv, input=input_data, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                          check=True, timeout=60).stdout.decode()


def container(root, service):
    """Сведения о контейнере сервиса проекта cellestial-services или пустой словарь."""
    found = run('docker', 'compose', '--project-name', 'cellestial-services', '--project-directory', str(root),
                '--file', str(root / 'compose.yml'), 'ps', '-q', service).strip()
    if not found:
        return {}
    info, = json.loads(run('docker', 'inspect', found))
    labels = info['Config']['Labels'] or {}
    return {'running': info['State']['Running'] is True, 'image_ref': info['Config']['Image'], 'image_id': info['Image'],
            'revision': labels.get('org.opencontainers.image.revision'),
            'source': labels.get('org.opencontainers.image.source')}


def schema_version(root, database, service):
    """Последняя применённая версия Goose схемы сервиса; только SELECT."""
    postgres = run('docker', 'compose', '--project-name', 'cellestial', '--project-directory', str(root.parent),
                   'ps', '-q', 'postgres').strip()
    sql = f'SELECT COALESCE(max(version_id),0) FROM {service}.goose_db_version WHERE is_applied;'
    return int(run('docker', 'exec', '-i', postgres, 'sh', '-ec',
                   'exec psql -XAt -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$1"', 'psql', database,
                   input_data=sql.encode()).strip())


def main(root):
    root = Path(root)
    with (root / 'deploy.lock').open('a') as lock:
        # Пока deploy.py держит блокировку, состояние меняется и сверка недостоверна.
        fcntl.flock(lock.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        database = json.loads((root / 'operation.json').read_text())['database']
        if not re.fullmatch('[a-z_][a-z0-9_]{0,62}', database):
            raise ValueError('unexpected database name')
        facts = {}
        for service in SERVICES:
            entry = {}
            try:
                entry.update(container(root, service))
                entry['schema_version'] = schema_version(root, database, service)
            except (OSError, ValueError, KeyError, subprocess.SubprocessError):
                pass
            facts[service] = entry
    print(json.dumps(facts))


if __name__ == '__main__':
    try:
        main(sys.argv[1])
    except (OSError, ValueError, KeyError, IndexError, subprocess.SubprocessError) as error:
        print(f'reconciliation evidence unavailable: {type(error).__name__}', file=sys.stderr)
        sys.exit(1)
