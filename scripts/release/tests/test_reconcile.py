"""Сверка VPS2 после прерванной выкладки (backend#117): без SSH, Docker и GitHub."""
import json
import os
import pathlib
import shutil
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1]))

import service_deploy
from service_deploy import SERVICES, SOURCE_URL, ReleaseError, execute, reconciled_inventory

OLD, NEW, TARGET = 'b' * 40, 'c' * 40, 'd' * 40
PROTOCOL = 'a' * 40


def version(service, sha, migration):
    """Версия сервиса в формате inventory выпуска."""
    result = {'image_ref': f'ghcr.io/cringe-driven-development-team/cellestial-{service}@sha256:{sha[0] * 64}',
              'image_id': 'sha256:' + sha[0] * 63 + service[0], 'source_sha': sha, 'platform': 'linux/amd64',
              'migration_version': migration, 'protocol_revision': PROTOCOL}
    if service == 'environments':
        result.update(kernel_ref='ghcr.io/cringe-driven-development-team/cellestial-kernel-python@sha256:' + '7' * 64,
                      agent_version=sha)
    return result


def fact(entry, schema=None, **change):
    """Сведения сверки о контейнере, запущенном из версии entry."""
    result = {'running': True, 'image_ref': entry['image_ref'], 'image_id': entry['image_id'],
              'revision': entry['source_sha'], 'source': SOURCE_URL,
              'schema_version': entry['migration_version'] if schema is None else schema}
    result.update(change)
    return result


class FakeGitHub:
    """GitHub с опубликованным Release цели и сравнением «вперёд»."""

    prefix = '/repos/test'

    def __init__(self, *_):
        self.posts = []

    def get(self, path):
        return {'status': 'ahead'}

    def pages(self, path, field=None):
        return [{'tag_name': 'release-' + TARGET, 'draft': False, 'prerelease': False}]

    def request(self, path, body=None, method=None):
        self.posts.append(path)
        raise AssertionError('reconciliation must not write to GitHub')

    def status(self, *_):
        raise AssertionError('reconciliation must not write to GitHub')


class Reconcile(unittest.TestCase):
    def setUp(self):
        self.before = {name: version(name, OLD, 2) for name in SERVICES}
        self.desired = {name: version(name, NEW, 3) for name in SERVICES}
        self.target = {name: version(name, TARGET, 3) for name in SERVICES}
        # Прервали после переключения identity: остальные остались на прежних образах и схемах
        self.facts = {'identity': fact(self.desired['identity']),
                      'workspace': fact(self.before['workspace']),
                      'environments': fact(self.before['environments']),
                      'onlineconf': fact(self.before['onlineconf'])}
        self.interrupted = {'run_id': 5, 'run_attempt': 1, 'before': self.before, 'desired': self.desired}

    def test_mixed_versions_are_confirmed_service_by_service(self):
        self.assertEqual(reconciled_inventory(self.facts, self.interrupted), {
            'identity': self.desired['identity'], 'workspace': self.before['workspace'],
            'environments': self.before['environments'], 'onlineconf': self.before['onlineconf']})

    def test_service_added_later_is_installed_for_the_first_time(self):
        # Прерванная операция была до OnlineConf: его контейнера нет, остальные подтверждаются
        older = {key: {name: version for name, version in self.interrupted[key].items() if name != 'onlineconf'}
                 for key in ('before', 'desired')}
        facts = {name: value for name, value in self.facts.items() if name != 'onlineconf'}
        before = reconciled_inventory(facts, {**self.interrupted, **older})
        self.assertNotIn('onlineconf', before)
        service_deploy.check_compatibility(before, {**before, **self.target}, list(SERVICES), first=False)
        with self.assertRaises(ReleaseError):
            service_deploy.check_compatibility(before, {**before, 'onlineconf': self.target['onlineconf']},
                                               [name for name in SERVICES if name != 'onlineconf'], first=False)
        # Запущенный неизвестный OnlineConf не принимается за первую установку
        with self.assertRaises(ReleaseError):
            reconciled_inventory({**facts, 'onlineconf': fact(self.target['onlineconf'])}, {**self.interrupted, **older})

    def test_discrepancies_stop_with_the_service_name(self):
        cases = {
            'workspace': ({**self.facts, 'workspace': {}}, 'контейнер не найден'),
            'identity': ({**self.facts, 'identity': fact(self.target['identity'])}, 'запущен ghcr.io'),
            'environments': ({**self.facts, 'environments': fact(self.before['environments'], schema=3)}, 'версия схемы 3'),
        }
        for name, (facts, message) in cases.items():
            with self.subTest(name=name), self.assertRaises(ReleaseError) as raised:
                reconciled_inventory(facts, self.interrupted)
            self.assertIn(f'{name}: {message}', str(raised.exception))
        for change in ({'image_id': 'sha256:' + '9' * 64}, {'revision': TARGET}, {'source': 'https://example.org'},
                       {'running': False}):
            with self.subTest(change=change), self.assertRaises(ReleaseError):
                reconciled_inventory({**self.facts, 'identity': fact(self.desired['identity'], **change)},
                                     self.interrupted)

    def run_cd(self, requested_tag, facts, observed, reconcile_expected=True):
        """Проходит prepare, begin и reconcile обычного CD после прерванной операции."""
        env = {'GITHUB_RUN_ID': '9', 'GITHUB_RUN_ATTEMPT': '1', 'RELEASE_ACTION': 'deploy', 'RELEASE_SERVICES': 'all',
               'RELEASE_TAG': requested_tag, 'GITHUB_REPOSITORY': service_deploy.REPOSITORY,
               'GITHUB_REF': 'refs/heads/main', 'PRODUCTION_ENABLED': 'true', 'GITHUB_EVENT_NAME': 'workflow_dispatch',
               'GITHUB_WORKFLOW_REF': service_deploy.REPOSITORY + '/.github/workflows/cd.yml@refs/heads/main',
               'GH_TOKEN': 'test', 'RELEASE_INFRA_SHA': 'e' * 40}
        prior = {**self.interrupted, 'observed': observed}
        directory = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, directory)
        path = pathlib.Path(directory) / 'state.json'
        with (patch.dict(os.environ, env, clear=True),
              patch.object(service_deploy, 'GitHub', FakeGitHub),
              patch.object(service_deploy, 'history', return_value=[{'id': 1}]),
              patch.object(service_deploy, 'read_deployment', return_value=('error', prior)),
              patch.object(service_deploy, 'manifest_versions', return_value=self.target),
              patch('service_artifacts.resolve', return_value={}),
              patch('service_artifacts.check_image'), patch('service_artifacts.pull_anonymous'),
              patch('artifacts.first_parent_main', return_value=['f' * 40, TARGET]),
              patch.object(service_deploy, 'inspect_vps', return_value=facts) as inspect):
            self.assertEqual(execute('init', path), 0)
            self.assertEqual(execute('prepare', path), 0)
            prepared = json.loads(path.read_text())
            if not reconcile_expected:
                return 0, prepared, inspect
            self.assertEqual(prepared['desired'], {})
            pathlib.Path(directory, 'copy.json').write_text(path.read_text())
            self.assertEqual(execute('begin', path), 1)
            path.write_text(pathlib.Path(directory, 'copy.json').read_text())
            code = execute('reconcile', path)
        return code, json.loads(path.read_text()), inspect

    def test_ordinary_cd_restores_the_exact_mix_and_continues(self):
        for tag, selection in (('release-' + TARGET, 'explicit'), ('  ', 'automatic')):
            for observed in ({}, {'identity': self.desired['identity']}):
                with self.subTest(tag=tag, observed=observed):
                    code, state, inspect = self.run_cd(tag, self.facts, observed)
                    self.assertEqual(code, 0)
                    inspect.assert_called_once()
                    self.assertEqual(state['release_selection'], selection)
                    self.assertEqual(state['target_sha'], TARGET)
                    self.assertEqual(state['before'], reconciled_inventory(self.facts, self.interrupted))
                    self.assertEqual(state['desired'], self.target)
                    self.assertIn('подтверждены identity', service_deploy.summary(state))

    def test_failed_reconciliation_changes_nothing_and_reports_the_service(self):
        facts = {**self.facts, 'workspace': {}}
        code, state, _ = self.run_cd('release-' + TARGET, facts, {})
        self.assertEqual(code, 1)
        self.assertEqual(state['error_stage'], 'reconcile')
        self.assertEqual(state['desired'], {})
        self.assertIsNone(state['deployment_id'])
        self.assertIn('workspace: контейнер не найден', service_deploy.summary(state))

    def test_first_onlineconf_install_after_a_complete_deployment_needs_no_reconciliation(self):
        # Последняя успешная выкладка была до OnlineConf: три сервиса, OnlineConf выбран впервые
        observed = {name: self.before[name] for name in SERVICES if name != 'onlineconf'}
        self.interrupted = {**self.interrupted, 'before': observed, 'desired': observed}
        code, state, inspect = self.run_cd('release-' + TARGET, self.facts, observed, reconcile_expected=False)
        self.assertEqual(code, 0)
        inspect.assert_not_called()
        self.assertNotIn('interrupted', state)
        self.assertEqual(state['before'], observed)
        self.assertEqual(state['desired'], self.target)

    def test_complete_observation_needs_no_vps_reconciliation(self):
        with patch.object(service_deploy, 'inspect_vps') as inspect:
            state = {'interrupted': None}
            state.pop('interrupted')
            service_deploy.reconcile(None, state)
        inspect.assert_not_called()


class Playbook(unittest.TestCase):
    def test_reconciliation_playbook_only_reads(self):
        import re
        root = pathlib.Path(__file__).resolve().parents[1]
        playbook = (root / 'reconcile.yml').read_text()
        self.assertEqual(set(re.findall(r'ansible\.builtin\.(\w+):', playbook)), {'assert', 'script', 'copy'})
        # Единственная запись — отчёт на контроллере CI
        self.assertIn('delegate_to: localhost', playbook.split('ansible.builtin.copy:', 1)[1])
        script = (root / 'inspect_services.py').read_text()
        for command in ("'up'", "'pull'", "'rm'", "'stop'", "'restart'", 'migrate', 'INSERT', 'UPDATE', 'DELETE'):
            self.assertNotIn(command, script)


if __name__ == '__main__':
    unittest.main()
