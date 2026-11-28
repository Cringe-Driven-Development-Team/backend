import importlib.util
import pathlib
import unittest

spec = importlib.util.spec_from_file_location('facts', pathlib.Path(__file__).resolve().parents[1] / 'facts.py')
facts = importlib.util.module_from_spec(spec)
spec.loader.exec_module(facts)

class CallbackFacts(unittest.TestCase):
    def test_image_and_local_health_are_independent(self):
        path = 'roles/app/tasks/check-backend-image.yml:1'
        image = 'sha256:' + 'a' * 64
        for health, expected_ready in [('healthy', 'passed'), ('unhealthy', 'failed')]:
            for selected, expected_image in [(image, 'passed'), ('sha256:' + 'b' * 64, 'failed')]:
                with self.subTest(health=health, selected=selected):
                    state = {'checks': {'image': 'unknown', 'ready': 'unknown'}}
                    facts.observe(state, path, 'Read the backend container image and health', 'ok',
                                  {'stdout': f'{image} true {health}'})
                    facts.observe(state, path, 'Read the local ID of the selected backend digest', 'ok',
                                  {'stdout': selected})
                    self.assertEqual(state['checks'], {'image': expected_image, 'ready': expected_ready})

    def test_untrusted_or_unreachable_health_is_not_confirmed(self):
        name = 'Read the backend container image and health'
        data = {'stdout': 'sha256:' + 'a' * 64 + ' true healthy'}
        state = {'checks': {'image': 'unknown', 'ready': 'unknown'}}
        facts.observe(state, 'fake.yml:1', name, 'ok', data)
        path = 'roles/app/tasks/check-backend-image.yml:1'
        facts.observe(state, path, name, 'ok', {**data, '_ansible_no_log': True})
        facts.observe(state, path, name, 'unreachable', data)
        self.assertEqual(state['checks'], {'image': 'unknown', 'ready': 'unknown'})
        self.assertEqual(state['error_stage'], 'ansible')
