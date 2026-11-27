"""Exercise actual workflow source gate and CLI refusal without network/VPS."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import textwrap
import unittest
from unittest.mock import patch
import sys
sys.path.insert(0, str(Path(__file__).resolve().parent))
from test_release import r, FakeGitHub, A, D

ROOT = Path(__file__).resolve().parents[3]


def workflow_block(source, heading):
    """Выделяет job или step по отступу заголовка."""
    lines = source.splitlines()
    start = lines.index(heading) + 1
    indent = len(heading) - len(heading.lstrip())
    end = next(
        (index for index in range(start, len(lines))
         if lines[index].strip() and not lines[index].startswith(' ' * (indent + 1))),
        len(lines),
    )
    return textwrap.dedent('\n'.join(lines[start:end]))


class Workflow(unittest.TestCase):
    def test_backend_release_path_uses_checked_out_source_on_trusted_push(self):
        ci = (ROOT / '.github/workflows/ci.yml').read_text()
        docker = workflow_block(ci, '  docker:')
        guard_name = '  - name: Check source for monolith release'
        gate = "steps.source.outputs.kind == 'monolith'"
        release_steps = [
            'Reuse published release or draft checkpoint', 'Login to GHCR',
            'Verify public package and anonymous digest access', 'Publish verified immutable Release',
        ]
        with self.subTest('production steps use the same gate'):
            for step in release_steps:
                self.assertIn(f'if: {gate}', workflow_block(docker, f'  - name: {step}').splitlines())
            export = workflow_block(docker, '  - name: Export deployment image reference')
            self.assertIn(f"if: {gate} && steps.preflight.outputs.build != 'false'", export.splitlines())
            build = workflow_block(docker, '  - name: Build and publish')
            self.assertIn("if: steps.source.outputs.kind != 'services' && steps.preflight.outputs.build != 'false'", build.splitlines())
            self.assertIn(f'  push: ${{{{ {gate} }}}}', build.splitlines())
            metadata = workflow_block(docker, '  - name: Image metadata')
            self.assertIn(f'type=raw,value=main,enable=${{{{ {gate} }}}}', metadata)
            self.assertIn('  source_kind: ${{ steps.source.outputs.kind }}', docker.splitlines())
            self.assertIn('  release_tag: ${{ steps.preflight.outputs.release_tag }}', docker.splitlines())
            dispatch = workflow_block(ci, '  dispatch-cd:')
            self.assertIn('needs: docker', dispatch.splitlines())
            self.assertIn("if: needs.docker.outputs.source_kind == 'monolith' "
                          "&& needs.docker.outputs.release_tag != '' && vars.PRODUCTION_ENABLED == 'true'",
                          dispatch.splitlines())

        with self.subTest('check builds and independent jobs'):
            self.assertNotRegex(docker, r'(?m)^if:')
            course = workflow_block(ci, '  course-sync:')
            self.assertIn('needs: [lint, test, docker, service-checks]', course.splitlines())
            self.assertNotIn('monolith_release', course)
            kernel = workflow_block(ci, '  kernel-images:')
            self.assertIn("push: ${{ env.PUBLISH == 'true' }}", kernel)
            self.assertNotIn('monolith_release', kernel)

        guard = workflow_block(docker, guard_name)
        self.assertLess(docker.index('  - uses: actions/checkout@'), docker.index(guard_name))
        self.assertIn('id: source', guard.splitlines())
        self.assertIn(f"if: github.repository == '{r.REPOSITORY}' && github.event_name == 'push' "
                      "&& github.ref == 'refs/heads/main'", guard.splitlines())
        self.assertIn('run: bash scripts/classify-backend-source.sh', guard.splitlines())

    def test_backend_source_classification_requires_complete_service_set(self):
        script = ROOT / 'scripts/classify-backend-source.sh'
        fake_git = """
git() {
    case "$*" in
        "rev-parse --verify HEAD") printf '%s\\n' "$CHECKOUT_SHA" ;;
        "ls-tree --name-only $CHECKOUT_SHA -- "*)
            if [[ -n "$SERVICE_ENTRYPOINTS" ]]; then
                printf '%s\\n' "$SERVICE_ENTRYPOINTS"
            fi
            ;;
        *) return 1 ;;
    esac
}
"""
        complete = '\n'.join([
            'cmd/identity/main.go', 'cmd/workspace/main.go', 'cmd/environments/main.go',
        ])
        for entrypoints, source_sha, expected, exit_code in [
            ('', A, 'kind=monolith\n', 0),
            (complete, A, 'kind=services\n', 0),
            ('cmd/identity/main.go', A, '', 1),
            ('cmd/workspace/main.go', A, '', 1),
            ('cmd/environments/main.go', A, '', 1),
            ('cmd/identity/main.go\ncmd/workspace/main.go', A, '', 1),
            ('', '0' * 40, '', 1),
        ]:
            with self.subTest(entrypoints=entrypoints, source_sha=source_sha), tempfile.TemporaryDirectory() as directory:
                output = Path(directory) / 'outputs'
                env = dict(os.environ, GITHUB_SHA=source_sha, CHECKOUT_SHA=A,
                           SERVICE_ENTRYPOINTS=entrypoints, GITHUB_OUTPUT=str(output))
                result = subprocess.run(['bash', '-c', fake_git + '\nsource "$1"', '_', str(script)],
                                        env=env, capture_output=True, text=True)
                self.assertEqual(result.returncode, exit_code, result.stderr)
                self.assertEqual(output.read_text() if output.exists() else '', expected)

    def test_service_checks_wait_for_complete_source(self):
        source = (ROOT / '.github/workflows/service-checks.yml').read_text()
        detection = workflow_block(source, '  source:')
        self.assertIn('run: bash scripts/classify-backend-source.sh', detection)
        for job in ['images', 'smoke']:
            with self.subTest(job=job):
                lines = workflow_block(source, f'  {job}:').splitlines()
                self.assertIn('needs: source', lines)
                self.assertIn("if: needs.source.outputs.kind == 'services'", lines)

    def test_invalid_target_and_history_failure_never_call_docker_or_ssh(self):
        for target, history_error in [('main', False), (A, True)]:
            with self.subTest(target=target, history_error=history_error), tempfile.TemporaryDirectory() as d:
                env={'RELEASE_STATE_FILE':d+'/state.json','RELEASE_ACTION':'rollback','RELEASE_TARGET_SHA':target,
                     'GITHUB_RUN_ID':'10','GITHUB_RUN_ATTEMPT':'1','GITHUB_REPOSITORY':r.REPOSITORY,
                     'GITHUB_REF':'refs/heads/main','GITHUB_EVENT_NAME':'workflow_dispatch','PRODUCTION_ENABLED':'true',
                     'GITHUB_WORKFLOW_REF':r.REPOSITORY+'/.github/workflows/rollback.yml@refs/heads/main','GH_TOKEN':'test','GITHUB_STEP_SUMMARY':d+'/summary','GITHUB_OUTPUT':d+'/outputs'}
                api=FakeGitHub();api.page_failure=history_error
                with patch.dict(os.environ,env,clear=True),patch.object(r,'GitHub',return_value=api),patch.object(r.subprocess,'run') as process:
                    self.assertEqual(r.main(['init']),0)
                    self.assertEqual(r.main(['prepare']),1)
                    self.assertEqual(r.main(['report']),0)
                    self.assertIn('История изменений неполная',Path(d+'/summary').read_text())
                    process.assert_not_called();self.assertEqual(api.posts,[])

    def test_rerun_does_not_read_previous_attempt_state(self):
        with tempfile.TemporaryDirectory() as d:
            r.save(d+'/state',r.initial_state('deploy',10,1))
            with patch.dict(os.environ,{'RELEASE_STATE_FILE':d+'/state','GITHUB_RUN_ID':'10','GITHUB_RUN_ATTEMPT':'2'}):
                with self.assertRaises(r.ReleaseError):r.main(['health'])

    def test_actual_gate_rejects_branch_repository_and_disabled_flag(self):
        import textwrap
        source=(ROOT/'.github/workflows/cd.yml').read_text()
        command=textwrap.dedent(source.split('        run: |\n',1)[1].split('      - name:',1)[0])
        for change in [{},{'GITHUB_REF':'refs/heads/topic'},{'PRODUCTION_ENABLED':'false'},{'GITHUB_REPOSITORY':'other/backend'}]:
            with tempfile.TemporaryDirectory() as d:
                env=dict(os.environ,RELEASE_ACTION='deploy',GITHUB_REPOSITORY=r.REPOSITORY,GITHUB_REF='refs/heads/main',
                         PRODUCTION_ENABLED='true',GITHUB_EVENT_NAME='workflow_dispatch',SOURCE_WORKFLOW=r.REPOSITORY+'/.github/workflows/cd.yml@refs/heads/main',
                         GITHUB_RUN_ID='10',GITHUB_RUN_ATTEMPT='1',GITHUB_STEP_SUMMARY=d+'/summary',GITHUB_ENV=d+'/env',RUNNER_TEMP=d)
                env.update(change)
                result=subprocess.run(['bash','-e','-c',command],env=env,capture_output=True)
                self.assertEqual(result.returncode==0,not bool(change))
                self.assertIn('состояние VPS не проверено',Path(d+'/summary').read_text())

    def test_cd_uses_manifest_not_dispatch_sha_and_repeats_current_release(self):
        import artifacts
        for base, target, comparison, expected in [(A,A,'identical',0),(A,'b'*40,'ahead',0),(A,'b'*40,'behind',1)]:
            with self.subTest(comparison=comparison), tempfile.TemporaryDirectory() as d:
                env={'RELEASE_STATE_FILE':d+'/state','RELEASE_ACTION':'deploy','RELEASE_TAG':'release-'+target,
                     'GITHUB_RUN_ID':'10','GITHUB_RUN_ATTEMPT':'1','GITHUB_REPOSITORY':r.REPOSITORY,
                     'GITHUB_REF':'refs/heads/main','GITHUB_EVENT_NAME':'workflow_dispatch','PRODUCTION_ENABLED':'true',
                     'GITHUB_WORKFLOW_REF':r.REPOSITORY+'/.github/workflows/cd.yml@refs/heads/main',
                     'GITHUB_SHA':'c'*40,'GH_TOKEN':'test','GITHUB_OUTPUT':d+'/outputs'}
                api=FakeGitHub();api.history=lambda:[{'sha':base,'image_ref':D,'state':'success','trusted':True}]
                api.get=lambda _: {'status':comparison}
                with patch.dict(os.environ,env,clear=True),patch.object(r,'GitHub',return_value=api),patch.object(artifacts,'resolve',return_value={'image_ref':D}) as resolve,patch.object(r,'check_image') as pull:
                    self.assertEqual(r.main(['init']),0)
                    self.assertEqual(r.main(['prepare']),expected)
                    if expected==0:
                        state=json.loads(Path(d+'/state').read_text())
                        self.assertEqual(state['target_sha'],target);self.assertEqual(state['image_ref'],D)
                        resolve.assert_called_once_with(api,'release-'+target);pull.assert_called_once_with(D)

    def test_rollback_refuses_release_digest_different_from_confirmed_history(self):
        import artifacts
        with tempfile.TemporaryDirectory() as d:
            env={'RELEASE_STATE_FILE':d+'/state','RELEASE_ACTION':'rollback','RELEASE_TARGET_SHA':A,
                 'GITHUB_RUN_ID':'10','GITHUB_RUN_ATTEMPT':'1','GITHUB_REPOSITORY':r.REPOSITORY,
                 'GITHUB_REF':'refs/heads/main','GITHUB_EVENT_NAME':'workflow_dispatch','PRODUCTION_ENABLED':'true',
                 'GITHUB_WORKFLOW_REF':r.REPOSITORY+'/.github/workflows/rollback.yml@refs/heads/main','GH_TOKEN':'test'}
            api=FakeGitHub();api.history=lambda:[{'sha':A,'image_ref':D,'state':'success','trusted':True}]
            with patch.dict(os.environ,env,clear=True),patch.object(r,'GitHub',return_value=api),patch.object(artifacts,'resolve',return_value={'image_ref':D[:-1]+'e'}),patch.object(r.subprocess,'run') as process:
                self.assertEqual(r.main(['init']),0);self.assertEqual(r.main(['prepare']),1)
                process.assert_not_called();self.assertEqual(api.posts,[])

    def test_repeated_switch_always_runs_ansible_with_unchanged_digest(self):
        with tempfile.TemporaryDirectory() as d:
            env={'RELEASE_STATE_FILE':d+'/state','RELEASE_ACTION':'deploy','GITHUB_RUN_ID':'10','GITHUB_RUN_ATTEMPT':'1',
                 'GITHUB_REPOSITORY':r.REPOSITORY,'GITHUB_REF':'refs/heads/main','GITHUB_EVENT_NAME':'workflow_dispatch',
                 'PRODUCTION_ENABLED':'true','GITHUB_WORKFLOW_REF':r.REPOSITORY+'/.github/workflows/cd.yml@refs/heads/main',
                 'ANSIBLE_DIR':d,'DEPLOY_KEY_PATH':d+'/key','JUMP_KNOWN_HOSTS':d+'/known'}
            state=r.initial_state('deploy',10,1);state.update(target_sha=A,image_ref=D,started=True)
            def switch(command,**kwargs):
                self.assertIn('deploy-backend.yml',command)
                extra=json.loads(Path(command[command.index('-e')+1][1:]).read_text())
                self.assertEqual(extra['backend_image_ref'],D)
                current=json.loads(Path(d+'/state').read_text());current['checks']['switch']='passed';r.save(d+'/state',current)
                return subprocess.CompletedProcess(command,0)
            r.save(d+'/state',state)
            with patch.dict(os.environ,env,clear=True),patch.object(r.subprocess,'check_output',side_effect=['b'*40,'c'*40]),patch.object(r.subprocess,'run',side_effect=switch) as process:
                self.assertEqual(r.main(['switch']),0);self.assertEqual(r.main(['switch']),0)
                self.assertEqual(process.call_count,2)
                final=json.loads(Path(d+'/state').read_text());self.assertEqual(final['infra_sha'],'c'*40)
                self.assertEqual(final['image_ref'],D)

    def test_dispatch_failure_preserves_ready_release_and_never_claims_deployment_success(self):
        import textwrap
        source=(ROOT/'.github/workflows/ci.yml').read_text()
        step = workflow_block(workflow_block(source, '  dispatch-cd:'), '  - name: Dispatch prepared Release to CD')
        command = textwrap.dedent(step.split('run: |\n', 1)[1])
        for code in (0,1):
            with tempfile.TemporaryDirectory() as d:
                env=dict(os.environ,GITHUB_STEP_SUMMARY=d+'/summary',GITHUB_REPOSITORY=r.REPOSITORY,RELEASE_TAG='release-'+A)
                script='gh() { return '+str(code)+'; }\n'+command
                result=subprocess.run(['bash','-e','-c',script],env=env,capture_output=True)
                self.assertEqual(result.returncode,code)
                report=Path(d+'/summary').read_text()
                self.assertIn('release-'+A,report)
                if code:
                    self.assertIn('remains prepared',report);self.assertIn('Run CD manually',report)
                else: self.assertIn('Follow the separate CD run',report)
