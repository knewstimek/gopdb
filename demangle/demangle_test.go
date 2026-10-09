package demangle

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

// testdata/undname_cases.tsv pairs decorated names (the fixture PDBs'
// publics and open-source library names) with undname.exe's rendering.
func TestUndnameCases(t *testing.T) {
	f, err := os.Open("testdata/undname_cases.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		mangled, want, _ := strings.Cut(strings.TrimRight(sc.Text(), "\r"), "\t")
		n++
		sym, err := Demangle(mangled)
		if err != nil {
			t.Errorf("%s: %v", mangled, err)
			continue
		}
		if got := sym.String(); got != want {
			t.Errorf("%s\n got  %q\n want %q", mangled, got, want)
		}
	}
	if n < 40 {
		t.Fatalf("only %d cases", n)
	}
}

func TestStructure(t *testing.T) {
	sym, err := Demangle("?loadTailoring@CollationLoader@icu_53@@SAPEBUCollationTailoring@2@AEBVLocale@2@AEAV42@AEAW4UErrorCode@@@Z")
	if err != nil {
		t.Fatal(err)
	}
	if sym.Kind != KindFunction || sym.QualifiedName() != "icu_53::CollationLoader::loadTailoring" {
		t.Fatalf("%+v", sym)
	}
	f := sym.Func
	if !f.Static || f.CallConv != "__cdecl" || len(f.Params) != 3 {
		t.Fatalf("%+v", f)
	}
	r := f.Return
	if r.Kind != TypePointer || !r.Ptr64 || r.Pointee.Kind != TypeNamed || r.Pointee.Name != "icu_53::CollationTailoring" || !r.Pointee.Const {
		t.Errorf("return %+v / %+v", r, r.Pointee)
	}
	p2 := f.Params[2]
	if p2.Kind != TypeReference || p2.Pointee.Tag != "enum" || p2.Pointee.Name != "UErrorCode" {
		t.Errorf("param 3 %+v / %+v", p2, p2.Pointee)
	}

	sym, err = Demangle("?m@C@@QBEHXZ")
	if err != nil || !sym.Func.Member || sym.Func.Static || sym.Func.ThisQuals != "const" || sym.Access != "public" {
		t.Errorf("?m@C@@QBEHXZ: %+v %+v %v", sym, sym.Func, err)
	}
	sym, err = Demangle("?h@@YAHHZZ")
	if err != nil || !sym.Func.Variadic || len(sym.Func.Params) != 1 {
		t.Errorf("?h@@YAHHZZ: %+v %v", sym.Func, err)
	}
	sym, err = Demangle("?g_handle@@3IA")
	if err != nil || sym.Kind != KindData || sym.Type.Name != "unsigned int" {
		t.Errorf("?g_handle@@3IA: %+v %v", sym, err)
	}
}

func TestRejects(t *testing.T) {
	for _, s := range []string{"", "main", "_main", "?", "?x", "?x@@", "?x@@Y", "?x@@YA", "?x@@3", "?$x@", "?x@@YAXPEA"} {
		if sym, err := Demangle(s); err == nil {
			t.Errorf("%q demangled to %q", s, sym.String())
		}
	}
}

// FuzzDemangle checks that no input panics or loops.
func FuzzDemangle(f *testing.F) {
	f.Add("?loadTailoring@CollationLoader@icu_53@@SAPEBUCollationTailoring@2@AEBVLocale@2@AEAV42@AEAW4UErrorCode@@@Z")
	f.Add("??$?R$0BLI@VPxJoint@physx@@@?$RepX@U?$R@VP@physx@@@physx@@QEAAXAEBU?$P@$0BLI@@@@Z")
	f.Fuzz(func(t *testing.T, s string) {
		if sym, err := Demangle(s); err == nil {
			_ = sym.String()
		}
	})
}
