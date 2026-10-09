"""Rebuild the fixture executables and PDBs in testdata/ with MSVC.

usage: py -3 testdata/src/build.py [path-to-MSVC-bin-HostX64]

Builds fixture.cpp for x64 and x86 without the C runtime (/NODEFAULTLIB,
/GS-, /GR-, own entry point) so no Windows SDK or CRT libraries are needed.

PDBs record absolute source, object and output paths (the toolchain resolves
subst drives and junctions to their real path). The build therefore runs in
a neutral scratch directory, C:\\gopdb-build, which is removed afterwards, so
the committed fixtures do not carry the builder's directory layout.
"""
import os
import shutil
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.dirname(HERE)
BIN = sys.argv[1] if len(sys.argv) > 1 else \
	r"C:\Program Files\Microsoft Visual Studio\2022\Community\VC\Tools\MSVC\14.38.33130\bin\Hostx64"
WORK = r"C:\gopdb-build"


def build(arch):
	tools = os.path.join(BIN, arch)
	name = "fixture_" + arch
	subprocess.run([os.path.join(tools, "cl.exe"), "/nologo", "/c", "/Zi", "/O1", "/GS-", "/GR-", "/EHs-c-",
		"/Fd" + name + "_cl.pdb", "/Fo" + name + ".obj", "fixture.cpp"], check=True, cwd=WORK)
	subprocess.run([os.path.join(tools, "link.exe"), "/nologo", "/DEBUG:FULL", "/NODEFAULTLIB", "/ENTRY:entry",
		"/SUBSYSTEM:CONSOLE", "/OPT:REF", "/INCREMENTAL:NO", "/PDBALTPATH:" + name + ".pdb", name + ".obj",
		"/OUT:" + name + ".exe", "/PDB:" + name + ".pdb"], check=True, cwd=WORK)
	for ext in (".exe", ".pdb"):
		shutil.copyfile(os.path.join(WORK, name + ext), os.path.join(OUT, name + ext))


if os.path.exists(WORK):
	sys.exit(WORK + " exists; remove it first")
os.makedirs(WORK)
try:
	shutil.copyfile(os.path.join(HERE, "fixture.cpp"), os.path.join(WORK, "fixture.cpp"))
	for a in ("x64", "x86"):
		build(a)
finally:
	shutil.rmtree(WORK, ignore_errors=True)
print("ok")
