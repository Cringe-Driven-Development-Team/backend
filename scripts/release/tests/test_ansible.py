"""Optional real callback loading test, using localhost asserts, no SSH or registry."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
sys.path.insert(0, str(Path(__file__).resolve().parent))
from test_release import r

class RealCallback(unittest.TestCase):
    @unittest.skipUnless(shutil.which('ansible-playbook'), 'ansible-playbook required for callback loading test')
    def test_ansible_loads_callback_and_preserves_partial_health(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);tasks=root/'roles/app/tasks';tasks.mkdir(parents=True)
            state=root/'state.json';r.save(state,r.initial_state('deploy',10,1))
            image = 'sha256:' + 'a' * 64
            (tasks/'check-backend-image.yml').write_text(f'''- name: Read the backend container image and health
  ansible.builtin.command: "echo {image} true unhealthy"
- name: Read the local ID of the selected backend digest
  ansible.builtin.command: "echo {image}"
- name: Backend image check
  ansible.builtin.assert:
    that: false
''')
            play=root/'play.yml';play.write_text('''- hosts: localhost
  connection: local
  gather_facts: false
  tasks:
    - ansible.builtin.import_tasks: roles/app/tasks/check-backend-image.yml
''')
            env=dict(os.environ,ANSIBLE_LOCAL_TEMP=str(root/'local'),ANSIBLE_REMOTE_TEMP=str(root/'remote'),
                     ANSIBLE_CALLBACK_PLUGINS=str(Path(__file__).resolve().parents[1]/'callback_plugins'),ANSIBLE_CALLBACKS_ENABLED='backend_result',
                     RELEASE_STATE_FILE=str(state),GITHUB_RUN_ID='10',GITHUB_RUN_ATTEMPT='1',ANSIBLE_NOCOLOR='1')
            result=subprocess.run(['ansible-playbook','-i','localhost,',str(play)],env=env,capture_output=True,timeout=30)
            self.assertNotEqual(result.returncode,0)
            actual=json.loads(state.read_text())
            self.assertEqual(actual['checks'], {'image': 'passed', 'ready': 'failed'})
