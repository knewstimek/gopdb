package demangle

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

// GOPDB_UNDNAME_TRUTH points at a file of "decorated<TAB>undname output"
// lines (undname.exe from Visual Studio, default flags). The test reports
// how many names this package renders identically, which ones it rejects,
// and the first mismatches.
func TestAgainstUndname(t *testing.T) {
	path := os.Getenv("GOPDB_UNDNAME_TRUTH")
	if path == "" {
		t.Skip("set GOPDB_UNDNAME_TRUTH")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var total, same, rejected int
	shown := 0
	reasons := map[string]int{}
	for sc.Scan() {
		mangled, want, ok := strings.Cut(sc.Text(), "\t")
		if !ok || want == mangled {
			continue // undname could not decode it either
		}
		total++
		sym, err := Demangle(mangled)
		if err != nil {
			rejected++
			msg := err.Error()
			if i := strings.LastIndex(msg, ": "); i >= 0 {
				msg = msg[i+2:]
			}
			reasons[msg]++
			continue
		}
		if got := sym.String(); got == want {
			same++
		} else if shown < 40 {
			shown++
			t.Logf("\n  %s\n  got  %s\n  want %s", mangled, got, want)
		}
	}
	t.Logf("%d names: %d identical (%.1f%%), %d rejected, %d differ; rejections %v",
		total, same, 100*float64(same)/float64(total), rejected, total-same-rejected, reasons)
}
