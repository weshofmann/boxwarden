import importlib.util
import io
import os
from pathlib import Path
import stat
import tarfile
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("diagnostic_build", Path(__file__).with_name("build.py"))
build = importlib.util.module_from_spec(spec)
spec.loader.exec_module(build)

class BuildContractTests(unittest.TestCase):
    def test_archive_is_exact_ustar_independent_of_input_metadata(self):
        with tempfile.TemporaryDirectory() as root:
            p=Path(root); binary=p/"binary"; binary.write_bytes(b"test bytes")
            first=p/"first.tar"; second=p/"second.tar"
            build.write_archive(binary,first)
            binary.chmod(0o700); os.utime(binary,(1234,1234))
            build.write_archive(binary,second)
            self.assertEqual(first.read_bytes(),second.read_bytes())
            with tarfile.open(first) as archive:
                self.assertEqual(archive.getnames(),["softnet"])
                m=archive.getmembers()[0]
                self.assertEqual((m.mode,m.uid,m.gid,m.mtime,m.uname,m.gname),(0o755,0,0,0,"",""))
                self.assertEqual(archive.extractfile(m).read(),b"test bytes")
            with self.assertRaises(FileExistsError): build.write_archive(binary,first)
    def test_unprivileged_binary_rejects_mode_owner_links_and_symlink(self):
        with tempfile.TemporaryDirectory() as root:
            p=Path(root)/"binary";p.write_bytes(b"binary");p.chmod(0o755)
            build.validate_binary_metadata(p.lstat(),os.getuid())
            for mode in (0o4550,0o2755,0o775,0o700):
                p.chmod(mode)
                with self.assertRaises(RuntimeError): build.validate_binary_metadata(p.lstat(),os.getuid())
            p.chmod(0o755)
            with self.assertRaises(RuntimeError): build.validate_binary_metadata(p.lstat(),os.getuid()+1)
            os.link(p,p.with_name("linked"))
            with self.assertRaises(RuntimeError): build.validate_binary_metadata(p.lstat(),os.getuid())
            p.with_name("linked").unlink();os.symlink(p,p.with_name("symlink"))
            with self.assertRaises(RuntimeError): build.validate_binary_metadata(p.with_name("symlink").lstat(),os.getuid())
    def test_mount_requires_exact_encrypted_backing_image_association(self):
        d={"VolumeUUID":build.VOLUME_UUID,"DeviceIdentifier":"disk11s1","APFSPhysicalStores":[{"APFSPhysicalStore":"disk10s2"}]}
        image={"image-path":build.IMAGE_PATH,"image-encrypted":True,"system-entities":[{"dev-entry":"/dev/disk11s1"},{"dev-entry":"/dev/disk10s2"}]}
        build.validate_mount(d,{"images":[image]})
        for change in ({"image-encrypted":False},{"image-path":"/wrong.sparsebundle"},{"system-entities":[{"dev-entry":"/dev/disk11s1"}]}):
            with self.assertRaises(RuntimeError): build.validate_mount(d,{"images":[dict(image,**change)]})
        with self.assertRaises(RuntimeError): build.validate_mount(dict(d,VolumeUUID="wrong"),{"images":[image]})
    def test_immutable_input_inventory_rejects_missing_extra_or_changed_bytes(self):
        original={"source.rs":"abc", "Cargo.lock":"def"}
        build.validate_inventory(dict(original),original)
        for observed in ({"source.rs":"abc"},dict(original,extra="ghi"),dict(original,**{"source.rs":"changed"})):
            with self.assertRaises(RuntimeError): build.validate_inventory(observed,original)

    def test_retention_preflight_refuses_cross_device_before_mutation(self):
        build.validate_retention_devices(10,10)
        with self.assertRaises(RuntimeError): build.validate_retention_devices(10,11)

    def test_actual_linker_argv_has_two_prefix_elements_and_conflicts_refuse(self):
        argv,env=build.transform_tool("ld","/pinned/ld",["-o","one path"],{"target":"/exact/target"},{"PATH":"/closed"})
        self.assertEqual(argv,["/pinned/ld","-oso_prefix","/exact/target","-o","one path"])
        self.assertEqual(env,{"PATH":"/closed"})
        for conflict in (["-oso_prefix","/other"],["-oso_prefix=/other"]):
            with self.assertRaises(RuntimeError):build.transform_tool("ld","/pinned/ld",conflict,{"target":"/exact/target"},{})
    def test_actual_ar_child_environment_always_sets_zero_archive_date(self):
        for operation in ("cq","s"):
            argv,env=build.transform_tool("ar","/pinned/ar",[operation,"archive"],{}, {"ZERO_AR_DATE":"wrong","PATH":"/closed"})
            self.assertEqual(argv,["/pinned/ar",operation,"archive"])
            self.assertEqual(env,{"ZERO_AR_DATE":"1","PATH":"/closed"})

    def test_argv_preserves_element_boundaries_and_bytes(self):
        argv=["/tool","space in one argument","$()`,`",""]
        record=build.argv_record(argv,{"PATH":"/fixed"},"/neutral")
        self.assertEqual(record["argv_count"],4)
        self.assertEqual(record["argv_hex"],[os.fsencode(v).hex() for v in argv])
        self.assertEqual(record["environment"],{"PATH":"/fixed"})

if __name__ == "__main__": unittest.main()
