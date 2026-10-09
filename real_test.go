package pdb

import (
	"os"
	"strings"
	"testing"
	"time"
)

// GOPDB_TEST_PDBS="a.pdb;b.pdb" decodes every module symbol and every type
// and id record of real PDBs and reports what stayed undecoded.
func TestRealPDBs(t *testing.T) {
	list := os.Getenv("GOPDB_TEST_PDBS")
	if list == "" {
		t.Skip("set GOPDB_TEST_PDBS to ;-separated PDB paths")
	}
	for _, path := range strings.Split(list, ";") {
		start := time.Now()
		f, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		d, err := f.DBI()
		if err != nil {
			t.Fatal(err)
		}
		procs, locals := 0, 0
		for _, m := range d.Modules {
			syms, err := f.ModuleSymbols(m)
			if err != nil {
				t.Errorf("%s: module %s: %v", path, m.Name, err)
				continue
			}
			for _, s := range syms {
				if p, ok := s.(*Procedure); ok {
					procs++
					locals += len(p.Locals)
				}
			}
		}
		symTime := time.Since(start)
		for _, get := range []func() (*TypeTable, error){f.Types, f.IDs} {
			tt, err := get()
			if err != nil {
				t.Fatal(err)
			}
			if tt == nil {
				continue
			}
			raw := map[Leaf]int{}
			var fails int
			var firstErr error
			for i := 0; i < tt.Len(); i++ {
				typ, err := tt.Lookup(tt.Begin + TypeIndex(i))
				if err != nil {
					fails++
					if firstErr == nil {
						firstErr = err
					}
					continue
				}
				if r, ok := typ.(*RawType); ok {
					raw[r.L]++
				}
				if fl, ok := typ.(*FieldList); ok {
					for _, x := range fl.Fields {
						if rf, ok := x.(*RawField); ok {
							raw[rf.L|0x10000>>4]++
						}
					}
				}
			}
			t.Logf("%s: %d records, %d decode errors (first: %v), undecoded leaves %v", path, tt.Len(), fails, firstErr, raw)
			if fails > 0 {
				t.Errorf("%s: %d records failed to decode", path, fails)
			}
		}
		gs, err := f.GlobalSymbols()
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: %d modules, %d procedures, %d locals, %d global records; symbols %v, total %v",
			path, len(d.Modules), procs, locals, len(gs), symTime, time.Since(start))
		f.Close()
	}
}
