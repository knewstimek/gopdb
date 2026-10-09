package pdb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// DBI is the debug info stream: the module list and the indices of the
// other symbol streams.
type DBI struct {
	Age     uint32
	Machine uint16 // IMAGE_FILE_MACHINE_* of the image
	// GlobalStream, PublicStream and SymRecordStream are stream indices;
	// SymRecordStream holds the global and public symbol records.
	GlobalStream, PublicStream, SymRecordStream int
	Modules                                     []Module
	// Sections are the image's section headers as the linker recorded
	// them; Segment n of a symbol address is Sections[n-1].
	Sections []SectionHeader
}

// Module is one compiland (object file or import library member).
type Module struct {
	Name, ObjectName string
	// SymbolStream is the module's symbol stream (-1 when it has none);
	// SymbolBytes is the size of its symbol records, signature included.
	SymbolStream int
	SymbolBytes  uint32
}

// SectionHeader is an IMAGE_SECTION_HEADER from the PDB.
type SectionHeader struct {
	Name            string
	VirtualSize     uint32
	VirtualAddress  uint32
	SizeOfRawData   uint32
	Characteristics uint32
}

// RVA converts a symbol address (1-based section index and offset) to a
// relative virtual address. OMAP-reordered images are not supported.
func (d *DBI) RVA(segment uint16, offset uint32) (uint32, bool) {
	if segment == 0 || int(segment) > len(d.Sections) {
		return 0, false
	}
	return d.Sections[segment-1].VirtualAddress + offset, true
}

// Optional debug header stream slots (DbgHeaderType).
const dbgSectionHdr = 5

// DBI parses the DBI stream (cached).
func (f *File) DBI() (*DBI, error) {
	if f.dbi != nil {
		return f.dbi, nil
	}
	s, err := f.Stream(StreamDBI)
	if err != nil {
		return nil, err
	}
	if len(s) < 64 {
		return nil, errors.New("pdb: no DBI stream")
	}
	le := binary.LittleEndian
	d := &DBI{
		Age:             le.Uint32(s[8:]),
		GlobalStream:    int(le.Uint16(s[12:])),
		PublicStream:    int(le.Uint16(s[16:])),
		SymRecordStream: int(le.Uint16(s[20:])),
		Machine:         le.Uint16(s[58:]),
	}
	// Substream sizes, in stream order after the 64-byte header.
	var sizes [8]int
	for i, off := range []int{24, 28, 32, 36, 40, 48, 52} {
		v := int(int32(le.Uint32(s[off:])))
		if v < 0 {
			return nil, fmt.Errorf("pdb: corrupt DBI substream size at %d", off)
		}
		sizes[i] = v
	}
	modInfo, secContr, secMap, srcInfo, tsMap, optDbg, ec := sizes[0], sizes[1], sizes[2], sizes[3], sizes[4], sizes[5], sizes[6]
	pos := 64
	if pos+modInfo > len(s) {
		return nil, errors.New("pdb: corrupt DBI module info size")
	}
	d.Modules = parseModules(s[pos : pos+modInfo])
	pos += modInfo + secContr + secMap + srcInfo + tsMap + ec
	if optDbg >= 2*(dbgSectionHdr+1) && pos+optDbg <= len(s) {
		idx := int(le.Uint16(s[pos+2*dbgSectionHdr:]))
		if idx != 0xffff {
			if hdrs, err := f.Stream(idx); err == nil {
				d.Sections = parseSectionHeaders(hdrs)
			}
		}
	}
	f.dbi = d
	return d, nil
}

// Module info entries: 64 fixed bytes, the module and object names, padding
// to 4.
func parseModules(b []byte) []Module {
	le := binary.LittleEndian
	var mods []Module
	for pos := 0; pos+64 <= len(b); {
		m := Module{SymbolStream: int(le.Uint16(b[pos+34:])), SymbolBytes: le.Uint32(b[pos+36:])}
		if m.SymbolStream == 0xffff {
			m.SymbolStream = -1
		}
		p := pos + 64
		names := [2]string{}
		for k := range names {
			i := bytes.IndexByte(b[p:], 0)
			if i < 0 {
				p = len(b)
				break
			}
			names[k] = string(b[p : p+i])
			p += i + 1
		}
		m.Name, m.ObjectName = names[0], names[1]
		mods = append(mods, m)
		pos = (p + 3) &^ 3
	}
	return mods
}

func parseSectionHeaders(b []byte) []SectionHeader {
	le := binary.LittleEndian
	var out []SectionHeader
	for p := 0; p+40 <= len(b); p += 40 {
		out = append(out, SectionHeader{
			Name:            cString(b[p : p+8]),
			VirtualSize:     le.Uint32(b[p+8:]),
			VirtualAddress:  le.Uint32(b[p+12:]),
			SizeOfRawData:   le.Uint32(b[p+16:]),
			Characteristics: le.Uint32(b[p+36:]),
		})
	}
	return out
}
