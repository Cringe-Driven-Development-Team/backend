"""Выкладка микросервисов. GitHub хранит историю, Ansible подтверждает состояние VPS."""

import json
import os
from pathlib import Path
import re
import subprocess
import tempfile

from release import GitHub, REPOSITORY, RUN_ROOT, ReleaseError, HistoricalRunMissing, md, save, source_guard, validate_sha

SERVICES = ('identity', 'workspace', 'environments')
CHECKS = ('image', 'ready')
TERMINAL = ('success', 'failure', 'error')


def parse_selection(value):
    if value == 'all':
        return list(SERVICES)
    requested = value.split(',')
    if len(set(requested)) != len(requested) or not set(requested).issubset(SERVICES):
        raise ReleaseError('Choose all or a comma-separated list of distinct backend services')
    return [service for service in SERVICES if service in requested]


def check_compatibility(before, desired, selected, *, first):
    if set(desired) != set(SERVICES) or len({v['protocol_revision'] for v in desired.values()}) != 1:
        raise ReleaseError('All deployed neighbours must have matching HTTP and gRPC revisions')
    if first:
        if before or selected != list(SERVICES):
            raise ReleaseError('First cutover requires all services and no previous service inventory')
        return
    if set(before) != set(SERVICES):
        raise ReleaseError('Service inventory is incomplete; reconcile it before deployment')
    if any(desired[service]['migration_version'] < before[service]['migration_version'] for service in selected):
        raise ReleaseError('Target schema is older than a running schema; use the documented restore procedure')


def same_version(left, right):
    return all(left.get(key) == right.get(key) for key in (
        'image_ref', 'source_sha', 'protocol_revision', 'migration_version',
    ))


def same_inventory(left, right):
    return left.keys() == right.keys() and all(same_version(left[service], right[service]) for service in right)


def result_outcome(selected, desired, observed, checks, ansible_rc):
    if not ansible_rc and ansible_rc is not None and same_inventory(observed, desired) and all(
        checks.get(service, {}).get(check) == 'passed' for service in selected for check in CHECKS
    ):
        return 'success'
    if any(checks.get(service, {}).get(check) == 'failed' for service in selected for check in CHECKS):
        return 'failure'
    return 'error'


def check_inventory(value):
    from service_artifacts import validate_image

    if not isinstance(value, dict) or not set(value).issubset(SERVICES):
        raise ReleaseError('Invalid service inventory')
    for name, version in value.items():
        if not isinstance(version, dict):
            raise ReleaseError('Invalid service version')
        validate_image(name, version.get('image_ref'))
        for key in ('source_sha', 'protocol_revision'):
            if validate_sha(version.get(key)) != version[key]:
                raise ReleaseError('Noncanonical service version')
        if type(version.get('migration_version')) is not int or version['migration_version'] < 1:
            raise ReleaseError('Invalid service migration version')
    return value


def report_snapshot(state):
    result = {key: state[key] for key in (
        'action', 'run_id', 'run_attempt', 'infra_sha', 'deployment_id', 'release_tag',
        'selected_services', 'before', 'desired', 'observed', 'checks',
    )}
    result.update(schema_version=2, source_sha=state['target_sha'])
    return result


def read_deployment(api, row):
    payload = row['payload']
    action = payload.get('action')
    target = validate_sha(row.get('sha'))
    run = re.fullmatch(re.escape(RUN_ROOT) + r'([1-9][0-9]*)', str(payload.get('run')))
    notification = payload.get('notification') or {}
    attempt = notification.get('run_attempt')
    infra_sha = validate_sha(payload.get('infra_sha'))
    if (action not in ('deploy', 'rollback') or row.get('task') != action
            or row.get('environment') != 'production' or row.get('ref') != target
            or row.get('creator', {}).get('login') != 'github-actions[bot]'
            or payload.get('release_tag') != 'release-' + target
            or not run or type(attempt) is not int or attempt < 1
            or notification.get('report_url') != run[0] + '#summary'):
        raise ReleaseError('Invalid microservice Deployment provenance')
    run_id = int(run[1])
    workflow = 'cd.yml' if action == 'deploy' else 'rollback.yml'
    metadata = api.get(f'/actions/runs/{run_id}/attempts/{attempt}')
    if (metadata.get('id') != run_id or metadata.get('run_attempt') != attempt
            or metadata.get('path') != f'.github/workflows/{workflow}'
            or metadata.get('workflow_id') != api.get(f'/actions/workflows/{workflow}').get('id')
            or metadata.get('head_branch') != 'main' or metadata.get('event') != 'workflow_dispatch'
            or any(metadata.get(key, {}).get('full_name') != REPOSITORY for key in ('repository', 'head_repository'))):
        raise ReleaseError('Untrusted microservice deployment workflow')
    deployment_id = row['id']
    statuses = api.pages(f'/deployments/{deployment_id}/statuses?per_page=100')
    status = next((entry for entry in statuses if entry.get('state') != 'inactive'), {})
    if (status.get('state') not in TERMINAL or status.get('creator', {}).get('login') != 'github-actions[bot]'
            or status.get('log_url') != f'{run[0]}/attempts/{attempt}'
            or status.get('environment_url') != 'https://cellestial.ru'):
        raise ReleaseError('Latest service Deployment has no confirmed terminal status')
    if status['state'] == 'success':
        job_name = 'deploy' if action == 'deploy' else 'rollback / deploy'
        jobs = api.pages(f'/actions/runs/{run_id}/attempts/{attempt}/jobs?per_page=100', 'jobs')
        matching = [job for job in jobs if job.get('name') == job_name]
        for step in ('Switch backend', 'Health check'):
            successes = [item for item in matching[0].get('steps', []) if item.get('name') == step] if len(matching) == 1 else []
            if len(successes) != 1 or successes[0].get('conclusion') != 'success':
                raise ReleaseError('Deployment success lacks verified switch and health steps')
    artifact_name = f'backend-deployment-{run_id}-{attempt}'
    artifacts = api.pages(f'/actions/runs/{run_id}/artifacts?name={artifact_name}&per_page=100', 'artifacts')
    matching = [artifact for artifact in artifacts if artifact.get('name') == artifact_name and artifact.get('expired') is False]
    if len(matching) != 1 or not 0 < matching[0].get('size_in_bytes', 0) < 262144:
        raise ReleaseError('Missing or ambiguous deployment observation')
    with tempfile.TemporaryDirectory(prefix='backend-observation-') as directory:
        subprocess.run(['gh', 'run', 'download', str(run_id), '--repo', REPOSITORY, '--name', artifact_name,
                        '--dir', directory], check=True, capture_output=True, timeout=60)
        path = Path(directory) / 'deployment-result.json'
        if path.stat().st_size >= 262144:
            raise ReleaseError('Deployment observation is too large')
        result = json.loads(path.read_text())
    expected = {'schema_version': 2, 'run_id': run_id, 'run_attempt': attempt, 'deployment_id': deployment_id,
                'source_sha': target, 'infra_sha': infra_sha,
                **{key: payload.get(key) for key in ('action', 'release_tag', 'selected_services', 'before', 'desired')}}
    if not isinstance(result, dict) or any(result.get(key) != value for key, value in expected.items()):
        raise ReleaseError('Deployment observation does not match the recorded operation')
    if parse_selection(','.join(result['selected_services'])) != result['selected_services']:
        raise ReleaseError('Deployment observation has a noncanonical service selection')
    for key in ('before', 'desired', 'observed'):
        check_inventory(result[key])
    checks = result.get('checks')
    if (not isinstance(checks, dict) or set(checks) != set(result['selected_services'])
            or any(not isinstance(values, dict) or set(values) - {'smoke'} != set(CHECKS)
                   or any(value not in ('passed', 'failed', 'unknown') for value in values.values())
                   for values in checks.values())):
        raise ReleaseError('Invalid service check observations')
    if status['state'] == 'success' and result_outcome(
        result['selected_services'], result['desired'], result['observed'], result['checks'], 0,
    ) != 'success':
        raise ReleaseError('Deployment success lacks verified service observations')
    return status['state'], result


def history(api):
    rows = api.pages('/deployments?environment=production&per_page=100')
    rows.sort(key=lambda row: (row.get('created_at', ''), row['id']), reverse=True)
    return [row for row in rows if isinstance(row.get('payload'), dict) and row['payload'].get('schema_version') == 2]


def manifest_versions(manifest):
    versions = {}
    for name, value in manifest['services'].items():
        version = {key: value[key] for key in ('image_ref', 'image_id', 'source_sha', 'platform', 'migration_version')}
        version['protocol_revision'] = manifest['protocol_revision']
        if name == 'environments':
            version.update(kernel_ref=value['kernel_ref'], agent_version=value['agent_version'])
        versions[name] = version
    return versions


def confirmed_inventory(received, before, desired):
    confirmed = {}
    for name, observed in received.items():
        for inventory in (desired, before):
            if name in inventory and same_version(inventory[name], observed):
                confirmed[name] = inventory[name]
                break
    return confirmed


def choose_rollback(api, rows, before, selected, target):
    for index, row in enumerate(rows):
        if target and row.get('sha') != target:
            continue
        try:
            status, result = read_deployment(api, row)
        except HistoricalRunMissing:
            if index == 0:
                raise
            continue
        if status != 'success':
            continue
        if any(not same_version(before[name], result['observed'][name]) for name in selected):
            return result['source_sha'], result['observed']
    raise ReleaseError('No prior confirmed deployment changes the selected service versions')


def prepare(api, state):
    from artifacts import forward_guard, tag_sha
    from service_artifacts import check_image, pull_anonymous, resolve

    selected = state['selected_services']
    first = state['first_cutover']
    rows = history(api)
    prior_status, prior = read_deployment(api, rows[0]) if rows else (None, {})
    if first and prior_status == 'success':
        raise ReleaseError('First cutover cannot repeat a successful service deployment')
    # The host validates its cutover marker and recovery evidence before changing data.
    before = {} if first else prior.get('observed', {})
    if state['action'] == 'rollback':
        if first or set(before) != set(SERVICES):
            raise ReleaseError('First cutover restores the monolith by the backup runbook')
        requested_sha = state['requested_sha']
        target, candidates = choose_rollback(api, rows, before, selected, validate_sha(requested_sha) if requested_sha else '')
        manifest_cache = {}
        for name in selected:
            version = candidates[name]
            sha = version['source_sha']
            if sha not in manifest_cache:
                manifest_cache[sha] = manifest_versions(resolve(api, 'release-' + sha))
            verified = manifest_cache[sha][name]
            if not same_version(version, verified):
                raise ReleaseError('Confirmed rollback version conflicts with immutable Release')
            candidates[name] = verified
    else:
        target = tag_sha(state['requested_tag'])
        candidates = manifest_versions(resolve(api, 'release-' + target))
        for name in selected:
            if name in before:
                forward_guard(before[name]['source_sha'], candidates[name]['source_sha'],
                              lambda base, goal: api.get(f'/compare/{base}...{goal}').get('status'))
    desired = {**before, **{name: candidates[name] for name in selected}}
    check_compatibility(before, desired, selected, first=first)
    state.update(before=before, desired=desired, target_sha=target, release_tag='release-' + target)
    for name in selected:
        version = desired[name]
        check_image(name, version['image_ref'], version['source_sha'],
                    kernel_ref=version.get('kernel_ref'), image_id=version['image_id'])
        if name == 'environments':
            pull_anonymous(version['kernel_ref'])


def ensure_deployment(api, state):
    run = RUN_ROOT + str(state['run_id'])
    payload = {key: state[key] for key in (
        'action', 'release_tag', 'infra_sha', 'selected_services', 'before', 'desired',
    )}
    payload.update(schema_version=2, run=run, notification={
        'run_attempt': state['run_attempt'], 'base_sha': None, 'report_url': run + '#summary',
    })
    def existing():
        matches = []
        for row in history(api):
            old = row['payload']
            if old.get('run') == run and (old.get('notification') or {}).get('run_attempt') == state['run_attempt']:
                if (old != payload or row.get('sha') != state['target_sha'] or row.get('ref') != state['target_sha']
                        or row.get('task') != state['action'] or row.get('creator', {}).get('login') != 'github-actions[bot]'):
                    raise ReleaseError('Conflicting service Deployment for operation key')
                matches.append(row['id'])
        if len(matches) > 1:
            raise ReleaseError('Multiple service Deployments for the current run attempt')
        return matches[0] if matches else None

    found = existing()
    if found:
        return found
    try:
        row, _ = api.request(api.prefix + '/deployments', {
            'ref': state['target_sha'], 'task': state['action'], 'environment': 'production',
            'auto_merge': False, 'required_contexts': [], 'production_environment': True, 'payload': payload,
        })
    except ReleaseError:
        found = existing()
        if found:
            return found
        raise
    if row.get('sha') != state['target_sha'] or type(row.get('id')) is not int:
        raise ReleaseError('Service Deployment resolved to an unexpected SHA')
    return row['id']


def run_playbook(state):
    with tempfile.TemporaryDirectory(prefix='backend-service-deploy-') as directory:
        directory = Path(directory)
        report = directory / 'observed.json'
        settings = directory / 'extra-vars.json'
        clean_start = state.get('clean_start', False)
        save(settings, {
            'microservices_selected_services': state['selected_services'],
            'microservices_before': state['before'], 'microservices_desired': state['desired'],
            'microservices_result_path': str(report), 'microservices_action': state['action'],
            'microservices_first_cutover': state['first_cutover'] and not clean_start,
            'microservices_clean_start': clean_start,
            'deploy_key_path': os.environ['DEPLOY_KEY_PATH'], 'jump_known_hosts': os.environ['JUMP_KNOWN_HOSTS'],
        })
        known_hosts = os.environ['JUMP_KNOWN_HOSTS']
        result = subprocess.run([
            'ansible-playbook', '-i', 'inventory/ci.yml', 'deploy-microservices.yml', '-e', '@' + str(settings),
            '--ssh-common-args=-o UserKnownHostsFile=' + known_hosts + ' -o StrictHostKeyChecking=yes',
        ], cwd=os.environ['ANSIBLE_DIR'], check=False)
        state['ansible_rc'] = result.returncode
        if report.is_file() and report.stat().st_size < 262144:
            observed = json.loads(report.read_text())
            state['observed'] = confirmed_inventory(
                check_inventory(observed['observed']), state['before'], state['desired'],
            )
            checks = observed.get('checks', {})
            for name in state['selected_services']:
                for check in CHECKS:
                    value = checks.get(name, {}).get(check)
                    if value in ('passed', 'failed'):
                        state['checks'][name][check] = value


def summary(state):
    lines = ['## Выкладка микросервисов', '',
             f'- Операция: {state["action"]}', f'- Выпуск: {md(state.get("release_tag") or "не выбран")}',
             f'- Deployment: {state.get("deployment_id") or "не создан"}',
             f'- Infra SHA: {state.get("infra_sha") or "не определён"}',
             f'- Записанный результат: {state.get("terminal_status") or "не подтверждён"}', '',
             '| Сервис | До | Запрошено | Наблюдалось | Образ / readiness |',
             '| --- | --- | --- | --- | --- |']
    if state.get('clean_start'):
        lines.insert(3, '- Первый запуск на отдельной пустой БД')
    for name in SERVICES:
        refs = [md(state[key].get(name, {}).get('image_ref') or 'неизвестно') for key in ('before', 'desired', 'observed')]
        labels = ' / '.join(state['checks'].get(name, {}).get(check, 'не запрошено') for check in CHECKS)
        lines.append(f'| {name} | {" | ".join(refs)} | {labels} |')
        ca = state['before'].get(name, {}).get('source_sha')
        cb = state['observed'].get(name, {}).get('source_sha')
        if ca and cb and ca != cb:
            a, b = (cb, ca) if state['action'] == 'rollback' else (ca, cb)
            lines.append(f'| ↳ {name} | [Изменения](https://github.com/{REPOSITORY}/compare/{a}...{b}) | | | |')
    if state['error_stage']:
        lines += ['', f'Сбой: {md(state["error_stage"])}. Сверьте наблюдения и используйте runbook восстановления.']
    return '\n'.join(lines) + '\n'


def execute(command, path):
    run_id, attempt = (int(os.environ[key]) for key in ('GITHUB_RUN_ID', 'GITHUB_RUN_ATTEMPT'))
    if command == 'init':
        selected = parse_selection(os.environ.get('RELEASE_SERVICES', 'all'))
        action = os.environ['RELEASE_ACTION']
        transfer = os.environ.get('RELEASE_FIRST_CUTOVER') == 'true'
        clean_start = os.environ.get('RELEASE_CLEAN_START') == 'true'
        if clean_start and (transfer or action != 'deploy' or set(selected) != set(SERVICES)):
            raise ReleaseError('Clean start requires all services, deploy, and first_cutover disabled')
        save(path, {
            'action': action, 'run_id': run_id, 'run_attempt': attempt,
            'requested_sha': os.environ.get('RELEASE_TARGET_SHA', ''), 'requested_tag': os.environ.get('RELEASE_TAG', ''),
            'selected_services': selected, 'first_cutover': transfer or clean_start, 'clean_start': clean_start,
            'before': {}, 'desired': {}, 'observed': {},
            'checks': {name: dict.fromkeys(CHECKS, 'unknown') for name in selected},
            'ansible_rc': None, 'error_stage': None, 'deployment_id': None,
        })
        return 0
    state = json.loads(path.read_text())
    if (state['run_id'], state['run_attempt']) != (run_id, attempt):
        raise ReleaseError('Release state belongs to a different attempt')
    source_guard(state['action'])
    try:
        if command in ('prepare', 'begin', 'finalize'):
            api = GitHub(os.environ.get('GH_TOKEN', ''))
        if command == 'prepare':
            prepare(api, state)
        elif command == 'begin':
            state['infra_sha'] = validate_sha(os.environ['RELEASE_INFRA_SHA'])
            state['deployment_id'] = ensure_deployment(api, state)
            save(path, state)
            api.status(state, 'in_progress')
            state['started'] = True
        elif command == 'switch':
            if not state.get('started'):
                raise ReleaseError('Deployment in_progress not confirmed')
            run_playbook(state)
            if state['ansible_rc'] != 0:
                raise ReleaseError('Switch failed; see observed service state and Ansible logs')
        elif command == 'health':
            if deployment_outcome(state) != 'success':
                raise ReleaseError('Service health is not fully confirmed')
        elif command == 'observation':
            if state['deployment_id']:
                save(os.environ['RELEASE_ARTIFACT_FILE'], report_snapshot(state))
                with open(os.environ['GITHUB_OUTPUT'], 'a') as output:
                    output.write('upload=true\n')
        elif command == 'finalize':
            if state['deployment_id']:
                outcome = deployment_outcome(state)
                if os.environ.get('RELEASE_ARTIFACT_UPLOADED') != 'success':
                    outcome = 'error'
                    state['error_stage'] = 'record_observation'
                api.status(state, outcome)
                state['terminal_status'] = outcome
        elif command == 'report':
            with open(os.environ['GITHUB_STEP_SUMMARY'], 'a') as output:
                output.write(summary(state))
    except (ReleaseError, OSError, subprocess.SubprocessError, KeyError, TypeError, ValueError) as error:
        state['error_stage'] = state['error_stage'] or command
        save(path, state)
        print('::error::Backend services ' + command + ': ' + (
            str(error) if isinstance(error, ReleaseError) else 'operation failed; inspect stage logs'))
        return 1
    save(path, state)
    return 0


def deployment_outcome(state):
    return result_outcome(state['selected_services'], state['desired'], state['observed'], state['checks'], state['ansible_rc'])
