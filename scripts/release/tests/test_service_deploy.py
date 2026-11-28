import json
import os
import pathlib
import sys
import tempfile
import unittest
from types import SimpleNamespace
from unittest.mock import patch

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1]))

from service_deploy import (
    ReleaseError, SERVICES, check_compatibility, check_inventory, confirmed_inventory,
    deployment_outcome, execute, manifest_versions, parse_selection, report_snapshot, result_outcome,
    run_playbook, same_inventory, summary,
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
        self.checks = {'identity': {'image': 'passed', 'ready': 'passed'}}

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

    def test_clean_start_uses_initial_inventory_and_rejects_incompatible_inputs(self):
        env = {'GITHUB_RUN_ID': '1', 'GITHUB_RUN_ATTEMPT': '1', 'RELEASE_ACTION': 'deploy',
               'RELEASE_SERVICES': 'all', 'RELEASE_CLEAN_START': 'true'}
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / 'state.json'
            with patch.dict(os.environ, env, clear=True):
                self.assertEqual(execute('init', path), 0)
                state = json.loads(path.read_text())
                self.assertTrue(state['first_cutover'])
                self.assertTrue(state['clean_start'])
            for invalid in ({'RELEASE_FIRST_CUTOVER': 'true'}, {'RELEASE_SERVICES': 'identity'},
                            {'RELEASE_ACTION': 'rollback'}):
                with patch.dict(os.environ, {**env, **invalid}, clear=True), self.assertRaises(ReleaseError):
                    execute('init', path)

    def test_success_requires_full_observed_inventory_and_completed_checks(self):
        self.assertEqual(result_outcome(['identity'], self.desired, self.desired, self.checks, 0), 'success')
        self.assertEqual(result_outcome(['identity'], self.desired, self.before, self.checks, 0), 'error')
        self.assertEqual(result_outcome(['identity'], self.desired, {}, self.checks, 0), 'error')
        self.assertEqual(result_outcome(['identity'], self.desired, self.desired, self.checks, 1), 'error')
        self.assertEqual(result_outcome(['identity'], self.desired, self.desired,
                                        {'identity': {'image': 'passed'}}, 0), 'error')
        self.assertEqual(result_outcome(['identity'], self.desired, self.desired,
                                        {**self.checks, 'workspace': {'ready': 'failed'}}, 0), 'success')
        self.checks['identity']['ready'] = 'failed'
        self.assertEqual(result_outcome(['identity'], self.desired, self.desired, self.checks, 1), 'failure')

    def test_deploy_and_rollback_consume_local_ansible_observations(self):
        fixture = pathlib.Path(__file__).parent / 'fixtures' / 'backend-microservices-contract.json'
        report = json.loads(fixture.read_text())['result']
        for action in ('deploy', 'rollback'):
            for ready, returncode, expected in [
                ('passed', 0, 'success'), ('failed', 0, 'failure'),
                (None, 0, 'error'), ('passed', 1, 'error'),
            ]:
                with self.subTest(action=action, ready=ready, returncode=returncode):
                    state = {**report, 'action': action, 'first_cutover': False, 'error_stage': None}
                    state['checks'] = {name: {'image': 'unknown', 'ready': 'unknown'}
                                       for name in state['selected_services']}
                    checks = {name: {'image': 'passed'} for name in state['selected_services']}
                    if ready is not None:
                        for service_checks in checks.values():
                            service_checks['ready'] = ready

                    def ansible(command, **_):
                        settings = json.loads(pathlib.Path(command[5][1:]).read_text())
                        self.assertEqual(settings['microservices_action'], action)
                        pathlib.Path(settings['microservices_result_path']).write_text(json.dumps({
                            'observed': report['observed'], 'checks': checks,
                        }))
                        return SimpleNamespace(returncode=returncode)

                    env = dict(ANSIBLE_DIR='.', DEPLOY_KEY_PATH='key', JUMP_KNOWN_HOSTS='known_hosts')
                    with patch.dict(os.environ, env), patch('service_deploy.subprocess.run', side_effect=ansible):
                        run_playbook(state)
                    self.assertEqual(deployment_outcome(state), expected)
                    self.assertIn('Образ / readiness', summary(state))
                    self.assertNotIn('BFF SHA', summary(state))

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
