# gopdb

A pure-Go reader for Microsoft PDB (Program Database) 7.0 files, the debug
information MSVC and clang-cl produce for Windows binaries. No cgo, no DIA SDK,
no dependencies outside the standard library.

```
go get github.com/knewstimek/gopdb
```

## What it reads

| Layer | Contents |
|---|---|
| MSF container | block-based stream directory, any stream by index |
| PDB info stream | version, signature, age, GUID (match against a PE's RSDS record) |
| DBI stream | modules (compilands), section headers, segment:offset to RVA |
| Module symbols | procedures with their flags (including no-return), parameters and locals (`S_REGREL32`, `S_BPREL32`, `S_LOCAL`, `S_FRAMEPROC`, blocks); records of inlined functions are kept apart; thunks (`S_THUNK32`) |
| Global symbols | publics (`S_PUB32`), global and static data, thread locals, procedure references, UDT names |
| TPI types | pointers, modifiers, procedures and member functions with calling conventions, argument lists, classes, structs, interfaces, unions, enums, arrays, bitfields, vtable shapes, field lists (members, bases, virtual bases, methods, method lists, static members, nested types, enumerators), forward-reference resolution, lookup by name |
| IPI ids | function and method ids, string ids, substring lists, build info, UDT source lines |
| Simple types | built-in types and their pointer modes (indices below 0x1000) |

Records the package does not interpret are returned as `*RawSymbol` /
`*RawType` with their bytes. Every type record of two large real-world PDBs
(370 MB, 1.4 million type records) decodes without falling back to raw, and
the whole file reads in under a second.

## Demangling

`github.com/knewstimek/gopdb/demangle` decodes MSVC decorated names -- the
`S_PUB32` names of code built without full debug info -- into structure: the
qualified name, and for functions the calling convention, return and
parameter types (for variables, the type). `Symbol.String()` renders it the
way `undname.exe` does: on 6,000 decorated names sampled from two large
real-world programs, 98.9% render identically, 1% are rejected as
unsupported (some thunks and template forms) and 0.1% differ.

```go
sym, err := demangle.Demangle("?loadTailoring@CollationLoader@icu_53@@SAPEBUCollationTailoring@2@AEBVLocale@2@AEAV42@AEAW4UErrorCode@@@Z")
// sym.QualifiedName() == "icu_53::CollationLoader::loadTailoring"
// sym.Func.CallConv == "__cdecl", sym.Func.Static, len(sym.Func.Params) == 3
// sym.String() == "public: static struct icu_53::CollationTailoring const * __ptr64 __cdecl ..."
```

## Example

```go
f, err := pdb.Open("app.pdb")
if err != nil {
	log.Fatal(err)
}
defer f.Close()

dbi, _ := f.DBI()
types, _ := f.Types()
for _, m := range dbi.Modules {
	syms, _ := f.ModuleSymbols(m)
	for _, s := range syms {
		p, ok := s.(*pdb.Procedure)
		if !ok {
			continue
		}
		rva, _ := dbi.RVA(p.Segment, p.Offset)
		typ, _ := types.Lookup(p.Type)
		fmt.Printf("%08x %s %T noreturn=%v\n", rva, p.Name, typ, p.Flags&pdb.ProcNoReturn != 0)
	}
}
```

Struct layout:

```go
node, _ := types.Lookup(types.Resolve(ti)) // follow a forward declaration
c := node.(*pdb.Class)
fields, _ := types.Fields(c.FieldList)     // follows LF_INDEX continuations
for _, f := range fields {
	if m, ok := f.(*pdb.Member); ok {
		fmt.Println(m.Offset, m.Name)
	}
}
```

## Not covered

- Writing PDBs, the hash streams (lookups are by scanning), and the
  `/names` string table
- OMAP address translation (images reordered after linking)
- C13 line-number and file-checksum subsections
- Older PDB 2.0 (JG) files

## Format references

- LLVM, [The PDB File Format](https://llvm.org/docs/PDB/index.html)
- Microsoft, [microsoft-pdb](https://github.com/microsoft/microsoft-pdb) (`cvinfo.h`)

## Tests

`testdata/` holds small x86 and x64 executables with their PDBs, built from
`testdata/src/fixture.cpp` by `testdata/src/build.py` (MSVC, no C runtime).
`GOPDB_TEST_PDBS="a.pdb;b.pdb" go test -run RealPDBs` decodes every record
of PDBs you supply and reports anything left undecoded.
`demangle/testdata/undname_cases.tsv` holds undname.exe's output for the
fixtures' names and open-source library names;
`GOPDB_UNDNAME_TRUTH=file go test ./demangle -run Undname` compares against
a larger file of your own (`decorated<TAB>undname output` lines).

## License

MIT
