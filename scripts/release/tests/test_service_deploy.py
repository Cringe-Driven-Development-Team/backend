import json
import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1]))

from service_deploy import (
    ReleaseError, SERVICES, check_compatibility, check_inventory, confirmed_inventory,
    manifest_versions, parse_selection, report_snapshot, result_outcome, same_inventory,
)
from service_artifacts import validate_manifest


class ServiceDeployment(unittest.TestCase):
    def setUp(self):
        self.before = {
            service: {'protocol_revision': 'a' * 40, 'migration_version': 2,
                      'image_ref': service + '-old', 'source_sha': 'b' * 40}
            for service in SERVICES
        }
        self.desired = {service: dict(version) for service, version in self.before.items()}
        self.desired['identity'].update(image_ref='identity-new', source_sha='c' * 40)
        self.checks = {'identity': {'image': 'passed', 'ready': 'passed', 'smoke': 'passed'}}

    def test_selection_rejects_duplicates_and_unknown_services(self):
        self.assertEqual(parse_selection('all'), list(SERVICES))
        self.assertEqual(parse_selection('workspace,identity'), ['identity', 'workspace'])
        for invalid in ('identity,identity', 'all,workspace', 'other', ''):
            with self.subTest(invalid=invalid), self.assertRaises(ReleaseError):
                parse_selection(invalid)

    def test_partial_requires_the_protocol_of_running_neighbours(self):
        check_compatibility(self.before, self.desired, ['identity'], first=False)
        self.desired['identity']['protocol_revision'] = 'd' * 40
        with self.assertRaises(ReleaseError):
            check_compatibility(self.before, self.desired, ['identity'], first=False)

    def test_first_cutover_and_database_downgrade_are_explicit(self):
        with self.assertRaises(ReleaseError):
            check_compatibility({}, self.desired, list(SERVICES), first=False)
        check_compatibility({}, self.desired, list(SERVICES), first=True)
        with self.assertRaises(ReleaseError):
            check_compatibility({}, self.desired, ['identity'], first=True)
        self.desired['identity']['migration_version'] = 1
        with self.assertRaises(ReleaseError):
            check_compatibility(self.before, self.desired, ['identity'], first=False)

    def test_success_requires_full_observed_inventory_and_completed_checks(self):
        self.assertEqual(result_outcome(['identity'], self.desired, self.desired, self.checks, 0), 'success')
        self.assertEqual(result_outcome(['identity'], self.desired, self.before, self.checks, 0), 'error')
        self.assertEqual(result_outcome(['identity'], self.desired, {}, self.checks, 0), 'error')
        self.assertEqual(result_outcome(['identity'], self.desired, self.desired, self.checks, 1), 'error')
        self.checks['identity']['ready'] = 'failed'
        self.assertEqual(result_outcome(['identity'], self.desired, self.desired, self.checks, 1), 'failure')

    def test_partial_observation_keeps_verified_runtime_artifact_metadata(self):
        env = self.before['environments']
        env['kernel_ref'] = 'expected-kernel-by-digest'
        env['agent_version'] = 'expected-agent-sha'
        self.desired['environments'] = dict(env)
        observed = {
            'identity': self.desired['identity'],
            'environments': dict(env, kernel_ref='untrusted-field'),
            'workspace': dict(self.before['workspace'], image_ref='unknown-container'),
        }
        self.assertEqual(confirmed_inventory(observed, self.before, self.desired), {
            'identity': self.desired['identity'], 'environments': env,
        })

    def test_shared_consumer_fixture_matches_deployment_report(self):
        path = pathlib.Path(__file__).parent / 'fixtures' / 'backend-microservices-contract.json'
        fixture = json.loads(path.read_text())
        report = fixture['result']
        deployment = fixture['deployment']
        tag = report['release_tag']
        manifest = validate_manifest(fixture['manifest'], tag)
        candidates = manifest_versions(manifest)
        combined = {**report['before'], **{name: candidates[name] for name in report['selected_services']}}
        self.assertTrue(same_inventory(combined, check_inventory(report['desired'])))
        check_compatibility(report['before'], combined, report['selected_services'], first=False)
        self.assertEqual(report_snapshot({**report, 'target_sha': deployment['sha']}), report)
        self.assertEqual(result_outcome(report['selected_services'], report['desired'], report['observed'],
                                        report['checks'], 0), 'success')
