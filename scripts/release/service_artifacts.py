#!/usr/bin/env python3
"""Публикует и проверяет неизменяемый выпуск трёх сервисов."""
import argparse
import gzip
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

from artifacts import digest, gh, lookup, output, producer_guard, tag_sha
from release import GitHub, ReleaseError, REPOSITORY, RUN_ROOT, validate_sha

SERVICES = ('identity', 'workspace', 'environments', 'onlineconf')
# Сервисы, добавленные после перехода на микросервисы: в старых выпусках их нет (backend#177).
ADDED = frozenset({'onlineconf'})
REGISTRY = 'ghcr.io/cringe-driven-development-team'
SOURCE_URL = 'https://github.com/' + REPOSITORY
KERNEL_PACKAGE = REGISTRY + '/cellestial-kernel-python'
PLATFORM = 'linux/amd64'
DIGEST = r'sha256:[0-9a-f]{64}'
FILES = ('release.json', *(f'{name}-image.tar.gz' for name in SERVICES), 'SHA256SUMS')


def package(service):
    if service not in SERVICES:
        raise ReleaseError('Unknown backend service')
    return REGISTRY + '/cellestial-' + service


def matches(pattern, value):
    return isinstance(value, str) and re.fullmatch(pattern, value) is not None


def validate_image(service, image_ref):
    if not matches(re.escape(package(service)) + '@' + DIGEST, image_ref):
        raise ReleaseError(f'Invalid {service} package or digest')
    return image_ref


def validate_kernel(kernel_ref):
    if not matches(re.escape(KERNEL_PACKAGE) + '@' + DIGEST, kernel_ref):
        raise ReleaseError('Invalid Python kernel package or digest')
    return kernel_ref


def archive_name(service):
    return f'{service}-image.tar.gz'


def validate_manifest(manifest, tag):
    source_sha = tag_sha(tag)
    if (not isinstance(manifest, dict) or type(manifest.get('format_version')) is not int
            or manifest['format_version'] != 2 or manifest.get('source_sha') != source_sha
            or not matches(re.escape(RUN_ROOT) + r'[1-9][0-9]*', manifest.get('ci_url'))
            or not matches(r'[0-9a-f]{40}', manifest.get('protocol_revision'))):
        raise ReleaseError('Invalid microservice release identity or format')
    services = manifest.get('services')
    if not isinstance(services, dict) or set(services) not in (set(SERVICES), set(SERVICES) - ADDED):
        raise ReleaseError('Release must describe all backend services')
    for service, version in services.items():
        if (not isinstance(version, dict) or version.get('source_sha') != source_sha
                or version.get('platform') != PLATFORM or not matches(DIGEST, version.get('image_id'))
                or type(version.get('migration_version')) is not int or version['migration_version'] < 1):
            raise ReleaseError(f'Invalid {service} build metadata')
        validate_image(service, version.get('image_ref'))
        archive = version.get('archive')
        if (not isinstance(archive, dict) or archive.get('name') != archive_name(service)
                or type(archive.get('size')) is not int or archive['size'] < 1
                or not matches(r'[0-9a-f]{64}', archive.get('sha256'))):
            raise ReleaseError(f'Invalid {service} archive metadata')
    environments = services['environments']
    validate_kernel(environments.get('kernel_ref'))
    if environments.get('agent_version') != source_sha:
        raise ReleaseError('Runtime agent version differs from release source')
    return manifest


def archive_metadata(path):
    return {'name': path.name, 'size': path.stat().st_size, 'sha256': digest(path)}


def sums(root):
    """Контрольные суммы manifest и архивов, присутствующих в каталоге выпуска."""
    return ''.join(f'{digest(root / name)}  {name}\n' for name in FILES[:-1] if (root / name).is_file())


def write_sums(root):
    (root / 'SHA256SUMS').write_text(sums(root))


def verify_files(root, tag):
    manifest = validate_manifest(json.loads((root / 'release.json').read_text()), tag)
    for service, entry in manifest['services'].items():
        if archive_metadata(root / archive_name(service)) != entry['archive']:
            raise ReleaseError(f'{service} archive differs from manifest')
    if (root / 'SHA256SUMS').read_text() != sums(root):
        raise ReleaseError('Microservice release SHA256SUMS mismatch')
    return manifest


def asset_names(release):
    names = [asset['name'] for asset in release.get('assets', [])]
    if any(names.count(name) > 1 for name in FILES):
        raise ReleaseError('Duplicate required release asset')
    return set(names)


def download(tag, root, names):
    for name in names:
        gh('release', 'download', tag, '--pattern', name, '--dir', str(root))


def resolve(api, tag):
    sha = tag_sha(tag)
    release = lookup(api, tag)
    if (not release or release.get('draft') is not False or release.get('prerelease') is not False
            or release.get('immutable') is not True or release.get('tag_name') != tag
            or not release.get('published_at')):
        raise ReleaseError('Microservice release is missing or not immutable')
    if api.get('/commits/' + tag).get('sha') != sha:
        raise ReleaseError('Microservice release tag does not resolve to its source SHA')
    names = asset_names(release)
    required = {name for name in FILES if not any(name == archive_name(service) for service in ADDED)}
    if not required.issubset(names):
        raise ReleaseError('Required microservice release assets are missing')
    with tempfile.TemporaryDirectory(prefix='service-release-') as directory:
        root = Path(directory)
        download(tag, root, tuple(name for name in FILES if name in names))
        return verify_files(root, tag)


def inspect_image(ref):
    result = subprocess.check_output(['docker', 'image', 'inspect', ref], text=True, timeout=30)
    images = json.loads(result)
    if not isinstance(images, list) or len(images) != 1 or not isinstance(images[0], dict):
        raise ReleaseError('Expected exactly one local image')
    return images[0]


def pull_anonymous(image_ref):
    with tempfile.TemporaryDirectory(prefix='service-anonymous-') as config:
        subprocess.run(['docker', '--config', config, 'pull', '--platform', PLATFORM, image_ref],
                       check=True, timeout=300)


def check_image(service, image_ref, source_sha, *, kernel_ref=None, image_id=None):
    validate_image(service, image_ref)
    pull_anonymous(image_ref)
    image = inspect_image(image_ref)
    config = image.get('Config') or {}
    labels = config.get('Labels') or {}
    actual_id = image.get('Id')
    if (image.get('Os') != 'linux' or image.get('Architecture') != 'amd64'
            or image_ref not in (image.get('RepoDigests') or [])
            or labels.get('org.opencontainers.image.source') != SOURCE_URL
            or labels.get('org.opencontainers.image.revision') != source_sha
            or not matches(DIGEST, actual_id) or (image_id is not None and actual_id != image_id)):
        raise ReleaseError(f'{service} image does not match the release metadata')
    if service == 'environments':
        env = set(config.get('Env') or [])
        if not kernel_ref:
            kernel_ref = next((item.split('=', 1)[1] for item in env
                               if item.startswith('RUNTIME_PYTHON_IMAGE=')), None)
        validate_kernel(kernel_ref)
        if {'RUNTIME_AGENT_VERSION=' + source_sha, 'RUNTIME_PYTHON_IMAGE=' + kernel_ref} - env:
            raise ReleaseError('Environments image has an unexpected agent or kernel version')
    return actual_id


def verify_public_package(service):
    name = package(service).rsplit('/', 1)[-1]
    endpoint = f'orgs/Cringe-Driven-Development-Team/packages/container/{name}'
    metadata = json.loads(subprocess.check_output(['gh', 'api', endpoint], text=True, timeout=30))
    if metadata.get('visibility') != 'public' or (metadata.get('repository') or {}).get('full_name') != REPOSITORY:
        raise ReleaseError(f'Package {name} must be public and linked to {REPOSITORY}')


def archive_image(service, image_ref, root, *, source_sha, kernel_ref=None):
    image_id = check_image(service, image_ref, source_sha, kernel_ref=kernel_ref)
    # Постоянное локальное имя делает архив воспроизводимым при повторном CI.
    local_tag = package(service) + ':release-archive'
    subprocess.run(['docker', 'tag', image_ref, local_tag], check=True, timeout=30)
    with tempfile.TemporaryFile() as raw:
        subprocess.run(['docker', 'save', local_tag], stdout=raw, check=True, timeout=300)
        raw.seek(0)
        with (root / archive_name(service)).open('wb') as target:
            with gzip.GzipFile(fileobj=target, mode='wb', mtime=0, filename='') as compressed:
                shutil.copyfileobj(raw, compressed)
    subprocess.run(['docker', 'image', 'rm', local_tag], check=True, timeout=30)
    with (root / archive_name(service)).open('rb') as archive:
        subprocess.run(['docker', 'load'], stdin=archive, check=True, timeout=300)
    if inspect_image(local_tag).get('Id') != image_id:
        raise ReleaseError(f'{service} docker load image differs from published build')
    return image_id


def source_metadata(source_sha):
    if subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip() != source_sha:
        raise ReleaseError('Release checkout differs from CI source')
    contracts = subprocess.check_output(['git', 'ls-tree', '-r', source_sha, '--', 'proto',
                                         'spec/identity', 'spec/workspace'])
    for directory in (b'proto/', b'spec/identity/', b'spec/workspace/'):
        if not any(line.split(b'\t', 1)[-1].startswith(directory) for line in contracts.splitlines()):
            raise ReleaseError('Microservice contract directory missing from source')
    revision = subprocess.check_output(['git', 'hash-object', '--stdin'], input=contracts).decode().strip()
    versions = {}
    for service in SERVICES:
        files = subprocess.check_output(['git', 'ls-tree', '--name-only', f'{source_sha}:migrations/{service}'],
                                        text=True).splitlines()
        versions[service] = max((int(name.split('_', 1)[0]) for name in files
                                 if matches(r'[0-9]+_[^/]+\.sql', name)), default=0)
        if not versions[service]:
            raise ReleaseError(f'No schema migrations for {service}')
    return revision, versions


def registry_digest(image_tag):
    result = subprocess.run(['docker', 'buildx', 'imagetools', 'inspect', '--format',
                             '{{json .Manifest}}', image_tag], text=True, capture_output=True, timeout=60)
    if result.returncode:
        # Только отсутствие тега разрешает сборку. Прочие ошибки останавливают выпуск.
        missing = re.fullmatch(re.escape(f'ERROR: {image_tag}: ') + '(not found|manifest unknown)',
                               result.stderr.strip())
        if missing:
            return None
        raise ReleaseError('Could not verify the current registry tag')
    try:
        descriptor = json.loads(result.stdout)
    except ValueError:
        raise ReleaseError('Registry returned an invalid manifest descriptor') from None
    value = descriptor.get('digest') if isinstance(descriptor, dict) else None
    if not matches(DIGEST, value):
        raise ReleaseError('Registry tag lacks a canonical digest')
    return value


def build_service(service, source_sha, kernel_ref):
    tag = package(service) + ':sha-' + source_sha
    image_digest = registry_digest(tag)
    if image_digest is None:
        subprocess.run(['docker', 'buildx', 'build', '--platform', PLATFORM, '--load',
                        '-f', 'build/services.Dockerfile', '--target', service, '--tag', tag,
                        '--label', 'org.opencontainers.image.source=' + SOURCE_URL,
                        '--label', 'org.opencontainers.image.revision=' + source_sha,
                        '--build-arg', 'RUNTIME_PYTHON_IMAGE=' + kernel_ref,
                        '--build-arg', 'RUNTIME_AGENT_VERSION=' + source_sha, '.'],
                       check=True, timeout=1200)
        subprocess.run(['bash', 'scripts/check-service-image.sh', service, tag], check=True, timeout=60)
        subprocess.run(['docker', 'push', tag], check=True, timeout=300)
        image_digest = registry_digest(tag)
        if image_digest is None:
            raise ReleaseError(f'{service} digest missing after push')
    return validate_image(service, package(service) + '@' + image_digest)


def check_existing_images(manifest):
    for service, entry in manifest['services'].items():
        verify_public_package(service)
        check_image(service, entry['image_ref'], manifest['source_sha'],
                    kernel_ref=entry.get('kernel_ref'), image_id=entry['image_id'])
    pull_anonymous(manifest['services']['environments']['kernel_ref'])


def create_draft(api, tag, sha, manifest):
    notes = api.request(api.prefix + '/releases/generate-notes', {'tag_name': tag, 'target_commitish': sha})[0]
    commands = '\n'.join('docker pull ' + manifest['services'][name]['image_ref'] for name in SERVICES
                         if name in manifest['services'])
    body = notes['body'] + f'\n\nSource: `{sha}`\n\nCI: {manifest["ci_url"]}\n\n```sh\n{commands}\n```\n'
    return api.request(api.prefix + '/releases', {'tag_name': tag, 'target_commitish': sha,
                       'name': 'Backend ' + tag, 'body': body, 'draft': True})[0]


def publish(api, tag, kernel_ref):
    producer_guard()
    sha = tag_sha(tag)
    if sha != validate_sha(os.environ['GITHUB_SHA']):
        raise ReleaseError('Publication target differs from CI source SHA')
    release = lookup(api, tag)
    if release and not release.get('draft'):
        manifest = resolve(api, tag)
        check_existing_images(manifest)
        return manifest
    with tempfile.TemporaryDirectory(prefix='service-release-') as directory:
        root = Path(directory)
        existing = asset_names(release) if release else set()
        if 'release.json' in existing:
            download(tag, root, tuple(name for name in FILES if name in existing))
            manifest = validate_manifest(json.loads((root / 'release.json').read_text()), tag)
            check_existing_images(manifest)
            for service, entry in manifest['services'].items():
                name = archive_name(service)
                if name not in existing:
                    image_id = archive_image(service, entry['image_ref'], root, source_sha=sha,
                                             kernel_ref=entry.get('kernel_ref'))
                    if image_id != entry['image_id']:
                        raise ReleaseError(f'{service} regenerated image differs from draft')
                if archive_metadata(root / name) != entry['archive']:
                    raise ReleaseError(f'{service} archive differs from draft')
        else:
            if existing.intersection(FILES):
                raise ReleaseError('Draft contains release assets without its manifest')
            validate_kernel(kernel_ref)
            pull_anonymous(kernel_ref)
            protocol, migrations = source_metadata(sha)
            manifest = {'format_version': 2, 'source_sha': sha, 'protocol_revision': protocol,
                        'ci_url': RUN_ROOT + os.environ['GITHUB_RUN_ID'], 'services': {}}
            refs = {service: build_service(service, sha, kernel_ref) for service in SERVICES}
            for service, ref in refs.items():
                verify_public_package(service)
                entry = {'image_ref': ref, 'source_sha': sha, 'platform': PLATFORM,
                         'migration_version': migrations[service]}
                if service == 'environments':
                    entry.update(agent_version=sha, kernel_ref=kernel_ref)
                entry['image_id'] = archive_image(service, ref, root, source_sha=sha, kernel_ref=entry.get('kernel_ref'))
                entry['archive'] = archive_metadata(root / archive_name(service))
                manifest['services'][service] = entry
            (root / 'release.json').write_text(json.dumps(manifest, indent=2) + '\n')
        if 'SHA256SUMS' not in existing:
            write_sums(root)
        verify_files(root, tag)
        if not release:
            release = create_draft(api, tag, sha, manifest)
        # Сначала сохраняем manifest, чтобы повтор использовал те же digest.
        for name in FILES:
            if name not in existing:
                gh('release', 'upload', tag, str(root / name))
        with tempfile.TemporaryDirectory(prefix='service-verify-') as verification:
            download(tag, Path(verification), FILES)
            verify_files(Path(verification), tag)
        api.request(api.prefix + '/releases/' + str(release['id']), {'draft': False}, method='PATCH')
    return resolve(api, tag)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('command', choices=['publish', 'kernel-digest'])
    args = parser.parse_args()
    sha = validate_sha(os.environ['GITHUB_SHA'])
    if args.command == 'kernel-digest':
        existing = registry_digest(KERNEL_PACKAGE + ':sha-' + sha)
        if existing:
            output(digest=existing)
        return
    tag = 'release-' + sha
    publish(GitHub(os.environ.get('GH_TOKEN', '')), tag, os.environ['BACKEND_KERNEL_REF'])
    output(release_tag=tag)
    with open(os.environ['GITHUB_STEP_SUMMARY'], 'a') as stream:
        stream.write(f'## Release микросервисов подготовлен\n\n'
                     f'[{tag}]({SOURCE_URL}/releases/tag/{tag})\n\nВыкладка запускается из CD.\n')


if __name__ == '__main__':
    try:
        main()
    except (ReleaseError, subprocess.SubprocessError, OSError, ValueError, KeyError) as error:
        # Выводим только безопасную диагностику без ответа CLI и переменных среды.
        diagnostic = str(error) if isinstance(error, ReleaseError) else type(error).__name__
        raise SystemExit(f'::error::Backend microservice release: {diagnostic}')
