import contextlib
import hashlib
import importlib.util
import io
import pathlib
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT=pathlib.Path(__file__).parent
spec=importlib.util.spec_from_file_location("recipe_v2",ROOT/"reproduce_v2.py")
recipe=importlib.util.module_from_spec(spec)
spec.loader.exec_module(recipe)

class BoundaryReached(Exception): pass

class ReproductionV2Tests(unittest.TestCase):
    def test_exact_source_and_version_are_bound(self):
        self.assertEqual(recipe.SOURCE_COMMIT,"85bcb9aebffcc4a8c98b1bec111df2db8c18e81f")
        self.assertEqual(recipe.SOURCE_MANIFEST_SHA,"c820aad5292d0a8cda78d57a3a87072b0e7cac67a1499d13436eb6c7854346c5")
        self.assertEqual(recipe.VERSION,"0.19.0-boxwarden-n1-diagnostic.2")
    def test_actual_path_gate_accepts_only_fresh_7_8_and_matching_outputs(self):
        with tempfile.TemporaryDirectory() as temp:
            mount=pathlib.Path(temp);external=mount/"n1build-20260930";external.mkdir(mode=0o700);(external/"outputs").mkdir(mode=0o700)
            for number in (5,6,7,8):(external/("rust-target-"+str(number))).mkdir(mode=0o700)
            def invoke(number,stage="task5-prearm-stage-r1",output=None):
                target=external/("rust-target-"+str(number))
                args=["recipe","--inputs",str(mount/"inputs"),"--target",str(target),"--stage",str(external/stage),"--output",str(external/"outputs"/(output or "task5-prearm-reproduction-"+str(number))),"--native-input",str(mount/"native.json")]
                with patch.object(recipe,"MOUNT",str(mount)),patch.object(recipe,"mount_proof",return_value={}),patch.object(recipe,"sha",side_effect=BoundaryReached),patch.object(sys,"argv",args),contextlib.redirect_stderr(io.StringIO()):recipe.main()
            for number in (7,8):
                with self.assertRaises(BoundaryReached):invoke(number)
            for number in (5,6):
                with self.assertRaises(SystemExit) as caught:invoke(number)
                self.assertEqual(caught.exception.code,2)
            for stage,output in (("task4a-stage-r3",None),("task5-prearm-stage-r1","foreign-output")):
                with self.assertRaises(SystemExit):invoke(7,stage,output)
            (external/"rust-target-7"/"not-empty").write_bytes(b"existing")
            with self.assertRaises(SystemExit):invoke(7)
    def test_recipe_drift_refuses_before_any_compiler_child(self):
        with tempfile.TemporaryDirectory() as temp:
            root=pathlib.Path(temp);copy=root/"recipe.py";copy.write_bytes((ROOT/"reproduce_v2.py").read_bytes())
            output=root/"output";output.mkdir(mode=0o700)
            boundary={"pinned_files":{str(copy):recipe.sha(copy)},"sdk_path":str(root),"sdk_resolved":str(root)}
            copy.write_bytes(copy.read_bytes()+b"\n# changed\n")
            with patch.object(recipe,"mount_proof",return_value={}),patch.object(recipe.subprocess,"run") as child:
                with self.assertRaisesRegex(RuntimeError,"compiler input changed"):
                    recipe.run_recorded(["/usr/bin/true"],{"PATH":"/closed"},root,output,"refused",boundary)
                child.assert_not_called()
            self.assertEqual(list(output.iterdir()),[])
    def test_compiler_element_boundaries_and_archive_date_remain(self):
        argv,env=recipe.transform_tool("ld","/pinned/ld",["-o","space in one argument"],{"target":"/fixed/target"},{"PATH":"/closed"})
        self.assertEqual(argv,["/pinned/ld","-oso_prefix","/fixed/target","-o","space in one argument"])
        self.assertEqual(env,{"PATH":"/closed"})
        argv,env=recipe.transform_tool("ar","/pinned/ar",["cq","archive"],{}, {"ZERO_AR_DATE":"wrong"})
        self.assertEqual(env,{"ZERO_AR_DATE":"1"})

if __name__=="__main__":unittest.main()
