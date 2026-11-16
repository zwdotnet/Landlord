import importlib.util
from pathlib import Path
import sys
import tempfile
import unittest

spec=importlib.util.spec_from_file_location('verify',Path(__file__).with_name('verify.py'))
verify=importlib.util.module_from_spec(spec);spec.loader.exec_module(verify)


class VerificationTests(unittest.TestCase):
    def run_child(self,source,kind=''):
        with tempfile.TemporaryDirectory() as temporary:
            log=Path(temporary)/'output.log'
            result=verify.execute([sys.executable,'-c',source],Path(temporary),log,kind)
            return result,log.read_text()

    def test_normal_stderr_is_not_failure(self):
        result,output=self.run_child("import sys;sys.stderr.write('normal progress\\n');print('Ran 2 tests in 0.1s\\n\\nOK')",'python')
        self.assertTrue(result['pass']);self.assertIn('normal progress',output)

    def test_nonzero_exit_still_fails_with_success_looking_output(self):
        result,_=self.run_child("import sys;print('Ran 2 tests in 0.1s\\n\\nOK');sys.exit(7)",'python')
        self.assertFalse(result['pass']);self.assertEqual(result['exit_code'],7)

    def test_empty_or_failed_test_output_does_not_pass(self):
        for kind,source in [('python',"print('no tests')"),('node',"print('# tests 2\\n# pass 1\\n# fail 1')"),('go',"print('{}')")]:
            result,_=self.run_child(source,kind);self.assertFalse(result['pass'])


if __name__=='__main__':unittest.main()
