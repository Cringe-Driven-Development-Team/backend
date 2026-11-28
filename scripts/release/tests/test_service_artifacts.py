"""Integrity and publication recovery for a complete microservice release."""
import copy
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import service_artifacts as services
from release import ReleaseError
from test_artifacts import ReleaseAPI, SHA

TAG = 'release-' + SHA
REGISTRY = 'ghcr.io/cringe-driven-development-team'
KERNEL = REGISTRY + '/cellestial-kernel-python@sha256:' + 'c' * 64
PROTOCOL = 'b' * 40
NAMES = ('identity', 'workspace', 'environments', 'onlineconf')
VERSIONS = {'identity': 1, 'workspace': 5, 'environments': 3, 'onlineconf': 2}
REFS = {name: REGISTRY + '/cellestial-' + name + '@sha256:' + char * 64
        for name, char in zip(NAMES, 'defa')}
IMAGE_ID = 'sha256:' + '9' * 64


def manifest():
    result = {
        'format_version': 2,
        'source_sha': SHA,
        'protocol_revision': PROTOCOL,
        'ci_url': services.RUN_ROOT + '10',
        'services': {},
    }
    for name in NAMES:
        result['services'][name] = {
            'image_ref': REFS[name], 'source_sha': SHA, 'platform': 'linux/amd64',
            'image_id': IMAGE_ID, 'migration_version': VERSIONS[name],
            'archive': {'name': f'{name}-image.tar.gz', 'size': len(name),
                        'sha256': hashlib.sha256(name.encode()).hexdigest()},
        }
    result['services']['environments'].update(agent_version=SHA, kernel_ref=KERNEL)
    return result


class Manifest(unittest.TestCase):
    def test_manifest_requires_each_service_package_and_source(self):
        valid = manifest()
        self.assertEqual(services.validate_manifest(valid, TAG), valid)
        for label, change in [
            ('missing service', lambda m: m['services'].pop('identity')),
            ('swapped package', lambda m: m['services']['workspace'].update(image_ref=REFS['identity'])),
            ('foreign source', lambda m: m['services']['identity'].update(source_sha='2' * 40)),
            ('noninteger migration', lambda m: m['services']['identity'].update(migration_version=True)),
            ('archive traversal', lambda m: m['services']['identity']['archive'].update(name='../identity-image.tar.gz')),
            ('mutable kernel', lambda m: m['services']['environments'].update(kernel_ref='cellestial-kernel-python:latest')),
            ('agent version mismatch', lambda m: m['services']['environments'].update(agent_version='7' * 40)),
        ]:
            with self.subTest(label=label):
                altered = copy.deepcopy(valid)
                change(altered)
                with self.assertRaises(ReleaseError):
                    services.validate_manifest(altered, TAG)

    def test_archive_checksums_are_enforced_even_if_transport_sums_are_recomputed(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name in NAMES:
                (root / f'{name}-image.tar.gz').write_bytes(name.encode())
            valid = manifest()
            (root / 'release.json').write_text(json.dumps(valid))
            services.write_sums(root)
            self.assertEqual(services.verify_files(root, TAG), valid)
            (root / 'identity-image.tar.gz').write_bytes(b'corrupted')
            services.write_sums(root)
            with self.assertRaisesRegex(ReleaseError, 'archive'):
                services.verify_files(root, TAG)


class Registry(unittest.TestCase):
    def test_tag_lookup_reads_buildx_manifest_descriptor(self):
        descriptor = {'schemaVersion': 2, 'mediaType': 'application/vnd.oci.image.index.v1+json',
                      'digest': IMAGE_ID, 'manifests': []}
        result = subprocess.CompletedProcess([], 0, stdout=json.dumps(descriptor), stderr='')
        with patch.object(services.subprocess, 'run', return_value=result) as inspect:
            self.assertEqual(services.registry_digest(KERNEL), IMAGE_ID)
        self.assertIn('{{json .Manifest}}', inspect.call_args.args[0])

    def test_registry_authentication_error_is_not_a_missing_tag(self):
        for error, missing in [(f'ERROR: {KERNEL}: not found', True),
                               (f'ERROR: {KERNEL}: denied: requested access to the resource is denied', False)]:
            with self.subTest(error=error):
                result = subprocess.CompletedProcess([], 1, stdout='', stderr=error)
                with patch.object(services.subprocess, 'run', return_value=result):
                    if missing:
                        self.assertIsNone(services.registry_digest(KERNEL))
                    else:
                        with self.assertRaises(ReleaseError):
                            services.registry_digest(KERNEL)


class Publication(unittest.TestCase):
    def setUp(self):
        self.api = ReleaseAPI()
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        env = {
            'GITHUB_REPOSITORY': services.REPOSITORY, 'GITHUB_REF': 'refs/heads/main',
            'GITHUB_EVENT_NAME': 'push', 'GITHUB_SHA': SHA, 'GITHUB_RUN_ID': '10',
            'GITHUB_WORKFLOW_REF': services.REPOSITORY + '/.github/workflows/ci.yml@refs/heads/main',
            'GITHUB_OUTPUT': directory.name + '/output',
        }
        self.addCleanup(patch.stopall)
        patch.dict(os.environ, env).start()
        patch.object(services, 'gh', side_effect=self.api.gh).start()
        patch.object(services, 'check_image', return_value=IMAGE_ID).start()
        patch.object(services, 'pull_anonymous').start()
        patch.object(services, 'verify_public_package').start()
        patch.object(services, 'source_metadata', return_value=(PROTOCOL, VERSIONS)).start()
        def archive(name, ref, root, **_):
            self.assertEqual(ref, REFS[name])
            (root / f'{name}-image.tar.gz').write_bytes(name.encode())
            return IMAGE_ID
        self.archive = patch.object(services, 'archive_image', side_effect=archive).start()
        self.build = patch.object(services, 'build_service', side_effect=lambda name, *_: REFS[name]).start()

    def test_publication_verifies_immutable_assets_and_uses_release_on_rerun(self):
        published = services.publish(self.api, TAG, KERNEL)
        self.assertEqual(published, manifest())
        self.assertEqual(services.resolve(self.api, TAG), published)
        self.assertEqual(set(self.api.assets), set(services.FILES))
        self.assertEqual(self.api.posts[-1][2], 'PATCH')
        number_of_requests = len(self.api.posts)
        self.archive.reset_mock()
        self.build.reset_mock()
        self.assertEqual(services.publish(self.api, TAG, KERNEL), published)
        self.assertEqual(len(self.api.posts), number_of_requests)
        self.archive.assert_not_called()
        self.build.assert_not_called()

    def test_interrupted_upload_keeps_digest_and_regenerates_only_missing_archives(self):
        original = self.api.gh

        def fail_workspace(*args):
            if args[:2] == ('release', 'upload') and Path(args[3]).name == 'workspace-image.tar.gz':
                raise subprocess.CalledProcessError(1, 'gh')
            return original(*args)

        with patch.object(services, 'gh', side_effect=fail_workspace):
            with self.assertRaises(subprocess.SubprocessError):
                services.publish(self.api, TAG, KERNEL)
        self.assertTrue(self.api.release['draft'])
        self.assertEqual(set(self.api.assets), {'release.json', 'identity-image.tar.gz'})
        self.archive.reset_mock()
        self.build.reset_mock()
        recovered = services.publish(self.api, TAG, REGISTRY + '/cellestial-kernel-python@sha256:' + '7' * 64)
        self.assertEqual(recovered['services']['environments']['kernel_ref'], KERNEL)
        self.assertEqual([call.args[0] for call in self.archive.call_args_list], ['workspace', 'environments', 'onlineconf'])
        self.build.assert_not_called()

    def test_release_before_onlineconf_is_still_valid(self):
        old = manifest()
        old['services'].pop('onlineconf')
        self.assertEqual(services.validate_manifest(old, TAG), old)
        broken = manifest()
        broken['services'].pop('workspace')
        with self.assertRaises(ReleaseError):
            services.validate_manifest(broken, TAG)

    def test_pr_cannot_publish(self):
        with patch.dict(os.environ, {'GITHUB_EVENT_NAME': 'pull_request'}):
            with self.assertRaises(ReleaseError):
                services.publish(self.api, TAG, KERNEL)
        self.assertEqual(self.api.posts, [])


class Workflow(unittest.TestCase):
    def test_publisher_waits_for_service_checks(self):
        root = Path(__file__).resolve().parents[3]
        ci = (root / '.github/workflows/ci.yml').read_text()
        publish = ci.split('\n  publish-services:\n', 1)[1].split('\n  course-sync:', 1)[0]
        self.assertIn('needs: [docker, service-checks, kernel-images]', publish)
        self.assertIn("needs.docker.outputs.source_kind == 'services'", publish)
        self.assertIn('python3 scripts/release/service_artifacts.py', publish)
        checks = (root / '.github/workflows/service-checks.yml').read_text()
        self.assertIn('  workflow_call:', checks)
        self.assertNotIn('packages: write', checks)
