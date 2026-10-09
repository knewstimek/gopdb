package pdb

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// The fixtures are testdata/src/fixture.cpp built by testdata/src/build.py
// (MSVC, /O1, no CRT) for x86 and x64.

type fixture struct {
	arch    string
	machine uint16
	ptr     uint64
}

var fixtures = []fixture{{"x86", 0x14c, 4}, {"x64", 0x8664, 8}}

func openFixture(t *testing.T, arch string) *File {
	t.Helper()
	f, err := Open(filepath.Join("testdata", "fixture_"+arch+".pdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

// rsdsGUID reads the PDB GUID the image's RSDS debug record names.
func rsdsGUID(t *testing.T, exe string) GUID {
	t.Helper()
	img, err := pe.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	for _, s := range img.Sections {
		data, _ := s.Data()
		if i := bytes.Index(data, []byte("RSDS")); i >= 0 && i+20 <= len(data) {
			var g GUID
			copy(g[:], data[i+4:i+20])
			return g
		}
	}
	t.Fatal("no RSDS record")
	return GUID{}
}

func procedures(t *testing.T, f *File) map[string]*Procedure {
	t.Helper()
	d, err := f.DBI()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*Procedure{}
	for _, m := range d.Modules {
		syms, err := f.ModuleSymbols(m)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range syms {
			if p, ok := s.(*Procedure); ok {
				out[p.Name] = p
			}
		}
	}
	return out
}

func TestInfoMatchesImage(t *testing.T) {
	for _, fx := range fixtures {
		f := openFixture(t, fx.arch)
		in, err := f.Info()
		if err != nil {
			t.Fatal(err)
		}
		if want := rsdsGUID(t, filepath.Join("testdata", "fixture_"+fx.arch+".exe")); in.GUID != want {
			t.Errorf("%s: GUID %s, image names %s", fx.arch, in.GUID, want)
		}
		d, err := f.DBI()
		if err != nil {
			t.Fatal(err)
		}
		if d.Machine != fx.machine || len(d.Sections) == 0 || d.Sections[0].Name != ".text" {
			t.Errorf("%s: machine 0x%x sections %v", fx.arch, d.Machine, d.Sections)
		}
	}
}

func TestProcedures(t *testing.T) {
	for _, fx := range fixtures {
		f := openFixture(t, fx.arch)
		d, _ := f.DBI()
		procs := procedures(t, f)
		text := d.Sections[0]
		for _, name := range []string{"entry", "sum_points", "scale", "std_call", "fast_call", "make_point", "use_node", "fatal", "geo::Rect::area"} {
			p := procs[name]
			if p == nil {
				t.Errorf("%s: no procedure %s", fx.arch, name)
				continue
			}
			rva, ok := d.RVA(p.Segment, p.Offset)
			if !ok || rva < text.VirtualAddress || rva+p.Length > text.VirtualAddress+text.VirtualSize {
				t.Errorf("%s: %s at RVA 0x%x outside .text", fx.arch, name, rva)
			}
		}
		if p := procs["fatal"]; p != nil && p.Flags&ProcNoReturn == 0 {
			t.Errorf("%s: fatal lacks the no-return flag (flags 0x%x)", fx.arch, p.Flags)
		}
		if p := procs["sum_points"]; p != nil {
			var params []string
			for _, l := range p.Locals {
				if v, ok := l.(*Local); ok && v.Flags&LocalIsParam != 0 {
					params = append(params, v.Name)
				}
			}
			if len(params) != 2 || params[0] != "pts" || params[1] != "n" {
				t.Errorf("%s: sum_points params %v", fx.arch, params)
			}
		}
	}
}

func TestProcedureTypes(t *testing.T) {
	for _, fx := range fixtures {
		f := openFixture(t, fx.arch)
		tt, err := f.Types()
		if err != nil {
			t.Fatal(err)
		}
		procs := procedures(t, f)
		proto := func(name string) *ProcedureType {
			typ, err := tt.Lookup(procs[name].Type)
			if err != nil {
				t.Fatalf("%s: %s type: %v", fx.arch, name, err)
			}
			p, ok := typ.(*ProcedureType)
			if !ok {
				t.Fatalf("%s: %s type is %T", fx.arch, name, typ)
			}
			return p
		}
		scale := proto("scale")
		typ, _ := tt.Lookup(scale.ArgList)
		args := typ.(*ArgList).Args
		var names []string
		for _, a := range args {
			st, _ := Simple(a)
			names = append(names, st.Name)
		}
		if ret, _ := Simple(scale.Return); ret.Name != "double" || len(names) != 3 || names[0] != "double" || names[1] != "float" || names[2] != "int" {
			t.Errorf("%s: scale is %v(%v)", fx.arch, scale.Return, names)
		}
		// x64 has one calling convention; the PDB records the C one there.
		wantStd, wantFast := CallNearStd, CallNearFast
		if fx.arch == "x64" {
			wantStd, wantFast = CallNearC, CallNearC
		}
		if got := proto("std_call").CallConv; got != wantStd {
			t.Errorf("%s: std_call conv 0x%x", fx.arch, got)
		}
		if got := proto("fast_call").CallConv; got != wantFast {
			t.Errorf("%s: fast_call conv 0x%x", fx.arch, got)
		}
		mtyp, _ := tt.Lookup(procs["geo::Rect::area"].Type)
		mf, ok := mtyp.(*MemberFunction)
		if !ok || mf.This == 0 {
			t.Fatalf("%s: geo::Rect::area type %#v", fx.arch, mtyp)
		}
		if fx.arch == "x86" && mf.CallConv != CallThis {
			t.Errorf("x86: geo::Rect::area conv 0x%x", mf.CallConv)
		}
		cls, _ := tt.Lookup(tt.Resolve(mf.Class))
		if c, ok := cls.(*Class); !ok || c.Name != "geo::Rect" || c.FwdRef() {
			t.Errorf("%s: method class %#v", fx.arch, cls)
		}
	}
}

func findUDT(t *testing.T, tt *TypeTable, name string) Type {
	t.Helper()
	for i := 0; i < tt.Len(); i++ {
		typ, err := tt.Lookup(tt.Begin + TypeIndex(i))
		if err != nil {
			continue
		}
		switch c := typ.(type) {
		case *Class:
			if c.Name == name && !c.FwdRef() {
				return c
			}
		case *Union:
			if c.Name == name && !c.FwdRef() {
				return c
			}
		case *Enum:
			if c.Name == name && !c.FwdRef() {
				return c
			}
		}
	}
	t.Fatalf("no definition of %s", name)
	return nil
}

func TestStructLayout(t *testing.T) {
	for _, fx := range fixtures {
		f := openFixture(t, fx.arch)
		tt, _ := f.Types()
		node := findUDT(t, tt, "Node").(*Class)
		fields, err := tt.Fields(node.FieldList)
		if err != nil {
			t.Fatal(err)
		}
		p := fx.ptr
		// next, pos[3], value, flags, color, name, callback, weight
		want := map[string]uint64{"next": 0, "pos": p, "value": p + 24, "flags": p + 28, "color": p + 32,
			"name": (p + 36 + p - 1) / p * p}
		want["callback"] = want["name"] + p
		want["weight"] = (want["callback"] + p + 7) / 8 * 8
		got := map[string]uint64{}
		for _, fl := range fields {
			if m, ok := fl.(*Member); ok {
				got[m.Name] = m.Offset
			}
		}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("%s: Node.%s at %d, want %d", fx.arch, k, got[k], v)
			}
		}
		if node.Size != want["weight"]+8 {
			t.Errorf("%s: Node size %d", fx.arch, node.Size)
		}

		flags := findUDT(t, tt, "Flags").(*Class)
		ff, _ := tt.Fields(flags.FieldList)
		var bits [][2]uint8
		for _, fl := range ff {
			bt, _ := tt.Lookup(fl.(*Member).Type)
			b := bt.(*Bitfield)
			bits = append(bits, [2]uint8{b.Position, b.Length})
		}
		if len(bits) != 3 || bits[0] != [2]uint8{0, 1} || bits[1] != [2]uint8{1, 3} || bits[2] != [2]uint8{4, 28} {
			t.Errorf("%s: Flags bitfields %v", fx.arch, bits)
		}

		if u := findUDT(t, tt, "Value").(*Union); u.Size != 4 {
			t.Errorf("%s: Value size %d", fx.arch, u.Size)
		}

		color := findUDT(t, tt, "Color").(*Enum)
		cf, _ := tt.Fields(color.FieldList)
		vals := map[string]uint64{}
		for _, fl := range cf {
			e := fl.(*Enumerate)
			vals[e.Name] = e.Value
		}
		if vals["Red"] != 1 || vals["Green"] != 2 || vals["Blue"] != 40000 {
			t.Errorf("%s: Color values %v", fx.arch, vals)
		}

		shape := findUDT(t, tt, "geo::Shape").(*Class)
		sf, _ := tt.Fields(shape.FieldList)
		var vtab, static bool
		for _, fl := range sf {
			switch fl.(type) {
			case *VFuncTab:
				vtab = true
			case *StaticMember:
				static = true
			}
		}
		if !vtab || !static {
			t.Errorf("%s: geo::Shape vfunctab=%v static=%v", fx.arch, vtab, static)
		}
		rect := findUDT(t, tt, "geo::Rect").(*Class)
		rf, _ := tt.Fields(rect.FieldList)
		if b, ok := rf[0].(*BaseClass); !ok || b.Offset != 0 {
			t.Errorf("%s: geo::Rect first member %#v", fx.arch, rf[0])
		}
	}
}

func TestGlobals(t *testing.T) {
	for _, fx := range fixtures {
		f := openFixture(t, fx.arch)
		syms, err := f.GlobalSymbols()
		if err != nil {
			t.Fatal(err)
		}
		tt, _ := f.Types()
		data := map[string]*Data{}
		pubs := 0
		for _, s := range syms {
			switch v := s.(type) {
			case *Data:
				data[v.Name] = v
			case *Public:
				pubs++
			}
		}
		if pubs == 0 {
			t.Errorf("%s: no publics", fx.arch)
		}
		for _, n := range []string{"g_nodes", "g_handle", "g_rect", "geo::Shape::count", "geo::Rect::`vftable'"} {
			if data[n] == nil {
				t.Errorf("%s: no global %s", fx.arch, n)
			}
		}
		if g := data["g_nodes"]; g != nil {
			typ, _ := tt.Lookup(g.Type)
			if a, ok := typ.(*Array); !ok || a.Size != 4*findUDT(t, tt, "Node").(*Class).Size {
				t.Errorf("%s: g_nodes type %#v", fx.arch, typ)
			}
		}
		if g := data["g_rect"]; g != nil {
			typ, _ := tt.Lookup(g.Type)
			ptr, ok := typ.(*Pointer)
			if !ok || uint64(ptr.Size) != fx.ptr {
				t.Fatalf("%s: g_rect type %#v", fx.arch, typ)
			}
			// The pointer names a forward declaration; Resolve finds the
			// definition.
			def, _ := tt.Lookup(tt.Resolve(ptr.Referent))
			if c, ok := def.(*Class); !ok || c.FwdRef() || c.Size != 2*fx.ptr+8 {
				t.Errorf("%s: g_rect points to %#v", fx.arch, def)
			}
		}
	}
}

func TestByName(t *testing.T) {
	for _, fx := range fixtures {
		tt, _ := openFixture(t, fx.arch).Types()
		for _, n := range []string{"Node", "geo::Rect", "Value", "Color"} {
			ti, ok := tt.ByName(n)
			if !ok {
				t.Errorf("%s: ByName(%s) not found", fx.arch, n)
				continue
			}
			if tt.Resolve(ti) != ti {
				t.Errorf("%s: ByName(%s) returned a forward declaration", fx.arch, n)
			}
		}
		if _, ok := tt.ByName("NoSuchType"); ok {
			t.Error("found NoSuchType")
		}
	}
}

func TestSimple(t *testing.T) {
	st, ok := Simple(0x74)
	if !ok || st.Name != "int" || st.Size != 4 || st.Class != SimpleSigned || st.PointerSize() != 0 {
		t.Errorf("0x74 = %+v", st)
	}
	st, ok = Simple(0x0670)
	if !ok || st.Name != "char" || st.PointerSize() != 8 {
		t.Errorf("0x670 = %+v", st)
	}
	if _, ok := Simple(0x1000); ok {
		t.Error("0x1000 is a record index")
	}
}

func TestRejectsNonPDB(t *testing.T) {
	dir := t.TempDir()
	for name, data := range map[string][]byte{
		"empty":     nil,
		"text":      []byte("not a program database, just some text long enough for a header read"),
		"truncated": msfMagic,
	} {
		path := filepath.Join(dir, name)
		os.WriteFile(path, data, 0o644)
		if f, err := Open(path); err == nil {
			f.Close()
			t.Errorf("%s: accepted", name)
		}
	}
	// A directory size beyond the limit is rejected before allocating.
	hdr := append([]byte{}, msfMagic...)
	hdr = binary.LittleEndian.AppendUint32(hdr, 4096) // block size
	hdr = binary.LittleEndian.AppendUint32(hdr, 1)    // free block map
	hdr = binary.LittleEndian.AppendUint32(hdr, 3)    // blocks
	hdr = binary.LittleEndian.AppendUint32(hdr, 0xfffffff0)
	hdr = binary.LittleEndian.AppendUint32(hdr, 0)
	hdr = binary.LittleEndian.AppendUint32(hdr, 2)
	path := filepath.Join(dir, "huge")
	os.WriteFile(path, hdr, 0o644)
	if f, err := Open(path); err == nil {
		f.Close()
		t.Error("huge directory accepted")
	}
}

func TestSymbolsStopsOnTruncation(t *testing.T) {
	rec := func(k SymbolKind, payload []byte) []byte {
		b := binary.LittleEndian.AppendUint16(nil, uint16(2+len(payload)))
		b = binary.LittleEndian.AppendUint16(b, uint16(k))
		return append(b, payload...)
	}
	pub := append(binary.LittleEndian.AppendUint32(nil, uint32(PublicFunction)), make([]byte, 6)...)
	pub = append(pub, "_main\x00"...)
	s := append(rec(SPub32, pub), 0xff, 0x7f, 0x0e, 0x11)
	syms := Symbols(s)
	if len(syms) != 1 || syms[0].(*Public).Name != "_main" {
		t.Fatalf("symbols %#v", syms)
	}
}
