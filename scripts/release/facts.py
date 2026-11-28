"""Собирает наблюдения об образе и готовности монолита из событий Ansible."""

import re


def observe(state, path, name, result, data):
    """Записывает проверенные факты Docker и этап ошибки без вывода команд."""
    checks = state['checks']
    path = path.rsplit(':', 1)[0].replace('\\', '/')
    if (path.endswith('roles/app/tasks/check-backend-image.yml') and result == 'ok'
            and not any(data.get(key) for key in ('_ansible_no_log', 'failed', 'skipped'))):
        if name == 'Read the backend container image and health':
            fields = data.get('stdout', '').split()
            if len(fields) == 3 and re.fullmatch(r'sha256:[0-9a-f]{64}', fields[0]):
                image_id, running, health = fields
                state['running_image_id'] = image_id if running == 'true' else None
                checks['ready'] = 'passed' if running == 'true' and health == 'healthy' else 'failed'
        elif name == 'Read the local ID of the selected backend digest':
            image_id = data.get('stdout', '').strip()
            if re.fullmatch(r'sha256:[0-9a-f]{64}', image_id):
                state['selected_image_id'] = image_id
        if state.get('running_image_id') and state.get('selected_image_id'):
            checks['image'] = ('passed' if state['running_image_id'] == state['selected_image_id']
                               else 'failed')
    if result in ('failed', 'unreachable') and not state.get('error_stage'):
        state['error_stage'] = 'ansible'
