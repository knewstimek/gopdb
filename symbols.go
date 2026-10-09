package pdb

import (
	"encoding/binary"
	"errors"
)

// SymbolKind is a CodeView symbol record kind (cvinfo.h SYM_ENUM_e).
type SymbolKind uint16

const (
	SEnd            SymbolKind = 0x0006
	SFrameProc      SymbolKind = 0x1012
	SThunk32        SymbolKind = 0x1102
	SBlock32        SymbolKind = 0x1103
	SLabel32        SymbolKind = 0x1105
	SConstant       SymbolKind = 0x1107
	SUDT            SymbolKind = 0x1108
	SBPRel32        SymbolKind = 0x110b
	SLData32        SymbolKind = 0x110c
	SGData32        SymbolKind = 0x110d
	SPub32          SymbolKind = 0x110e
	SLProc32        SymbolKind = 0x110f
	SGProc32        SymbolKind = 0x1110
	SRegRel32       SymbolKind = 0x1111
	SLThread32      SymbolKind = 0x1112
	SGThread32      SymbolKind = 0x1113
	SProcRef        SymbolKind = 0x1125
	SDataRef        SymbolKind = 0x1126
	SLProcRef       SymbolKind = 0x1127
	SSepCode        SymbolKind = 0x1132
	SLocal          SymbolKind = 0x113e
	SLProc32ID      SymbolKind = 0x1146
	SGProc32ID      SymbolKind = 0x1147
	SInlineSite     SymbolKind = 0x114d
	SInlineSiteEnd  SymbolKind = 0x114e
	SProcIDEnd      SymbolKind = 0x114f
	SInlineSite2    SymbolKind = 0x115d
	SDefRangeRegRel SymbolKind = 0x1145
)

// Symbol is one decoded symbol record. Records this package does not decode
// are returned as *RawSymbol.
type Symbol interface {
	Kind() SymbolKind
}

// RawSymbol is a record kept undecoded.
type RawSymbol struct {
	K    SymbolKind
	Data []byte // the record after its kind field
}

func (s *RawSymbol) Kind() SymbolKind { return s.K }

// ProcFlags are a procedure's CV_PROCFLAGS.
type ProcFlags uint8

const (
	ProcNoFPO      ProcFlags = 0x01
	ProcInterrupt  ProcFlags = 0x02
	ProcFar        ProcFlags = 0x04
	ProcNoReturn   ProcFlags = 0x08 // the function never returns
	ProcNotReached ProcFlags = 0x10
	ProcCustomCall ProcFlags = 0x20
	ProcNoInline   ProcFlags = 0x40
	ProcOptDbgInfo ProcFlags = 0x80
)

// Procedure is S_GPROC32/S_LPROC32 (and the _ID forms, whose Type is an IPI
// index instead of a TPI one).
type Procedure struct {
	K      SymbolKind
	Name   string // undecorated and scope-qualified, e.g. "ns::Class::Method"
	Offset uint32
	// Segment is the 1-based section index (see DBI.RVA).
	Segment  uint16
	Length   uint32 // code bytes
	Type     TypeIndex
	Flags    ProcFlags
	DbgStart uint32 // prologue end, relative to the start
	DbgEnd   uint32 // epilogue start, relative to the start
	// Locals are the procedure's own frame and register records (S_REGREL32,
	// S_BPREL32, S_LOCAL, S_FRAMEPROC, S_BLOCK32, ...) in record order.
	// Records that belong to functions inlined into it are left out.
	Locals []Symbol
}

func (s *Procedure) Kind() SymbolKind { return s.K }

// Global reports an externally visible procedure.
func (s *Procedure) Global() bool { return s.K == SGProc32 || s.K == SGProc32ID }

// TypeIsID reports that Type indexes the IPI stream (an LF_FUNC_ID or
// LF_MFUNC_ID record that names the TPI procedure type).
func (s *Procedure) TypeIsID() bool { return s.K == SGProc32ID || s.K == SLProc32ID }

// PublicFlags are CV_PUBSYMFLAGS.
type PublicFlags uint32

const (
	PublicCode     PublicFlags = 0x1
	PublicFunction PublicFlags = 0x2
	PublicManaged  PublicFlags = 0x4
	PublicMSIL     PublicFlags = 0x8
)

// Public is S_PUB32: a linker (usually decorated) name.
type Public struct {
	Name    string
	Flags   PublicFlags
	Offset  uint32
	Segment uint16
}

func (s *Public) Kind() SymbolKind { return SPub32 }

// Data is S_GDATA32/S_LDATA32 (and the thread-local S_GTHREAD32/S_LTHREAD32).
type Data struct {
	K       SymbolKind
	Name    string
	Type    TypeIndex
	Offset  uint32
	Segment uint16
}

func (s *Data) Kind() SymbolKind { return s.K }

// RegRel is S_REGREL32: a variable at a register-relative address.
type RegRel struct {
	Name     string
	Type     TypeIndex
	Register uint16 // CV_HREG_e
	Offset   int32
}

func (s *RegRel) Kind() SymbolKind { return SRegRel32 }

// BPRel is S_BPREL32: a variable relative to the frame pointer.
type BPRel struct {
	Name   string
	Type   TypeIndex
	Offset int32
}

func (s *BPRel) Kind() SymbolKind { return SBPRel32 }

// LocalFlags are CV_LVARFLAGS.
type LocalFlags uint16

const (
	LocalIsParam         LocalFlags = 0x001
	LocalAddrTaken       LocalFlags = 0x002
	LocalCompilerGen     LocalFlags = 0x004
	LocalOptimizedOut    LocalFlags = 0x100
	LocalIsEnregGlobal   LocalFlags = 0x200
	LocalIsEnregStatic   LocalFlags = 0x400
	LocalIsAggregate     LocalFlags = 0x008
	LocalIsAliased       LocalFlags = 0x020
	LocalIsAlias         LocalFlags = 0x040
	LocalIsReturnValue   LocalFlags = 0x080
	LocalIsAggregatedOut LocalFlags = 0x010
)

// Local is S_LOCAL: a variable whose location is given by the S_DEFRANGE_*
// records that follow it (optimized code).
type Local struct {
	Name  string
	Type  TypeIndex
	Flags LocalFlags
}

func (s *Local) Kind() SymbolKind { return SLocal }

// FrameProc is S_FRAMEPROC: the procedure's frame layout.
type FrameProc struct {
	FrameSize, PadSize, PadOffset, SaveRegsSize uint32
	Flags                                       uint32
}

func (s *FrameProc) Kind() SymbolKind { return SFrameProc }

// UDT is S_UDT: a name for a type (typedef or tag name).
type UDT struct {
	Name string
	Type TypeIndex
}

func (s *UDT) Kind() SymbolKind { return SUDT }

// ProcRef is S_PROCREF/S_LPROCREF: where a procedure's full record lives.
type ProcRef struct {
	K      SymbolKind
	Name   string
	Module uint16 // 1-based module index
	Offset uint32 // record offset in the module's symbol stream
}

func (s *ProcRef) Kind() SymbolKind { return s.K }

// Symbols decodes a symbol record stream: each record is [u16 length]
// [u16 kind][length-2 bytes]. Decoding stops at the first truncated record.
func Symbols(b []byte) []Symbol {
	var out []Symbol
	walk(b, 0, func(off int, k SymbolKind, rec []byte) { out = append(out, decodeSymbol(k, rec)) })
	return out
}

func walk(b []byte, base int, fn func(off int, k SymbolKind, rec []byte)) {
	le := binary.LittleEndian
	for pos := 0; pos+4 <= len(b); {
		n := int(le.Uint16(b[pos:]))
		if n < 2 || pos+2+n > len(b) {
			return
		}
		fn(base+pos, SymbolKind(le.Uint16(b[pos+2:])), b[pos+4:pos+2+n])
		pos += 2 + n
	}
}

func decodeSymbol(k SymbolKind, r []byte) Symbol {
	le := binary.LittleEndian
	switch k {
	case SGProc32, SLProc32, SGProc32ID, SLProc32ID:
		if len(r) >= 35 {
			return &Procedure{K: k, Length: le.Uint32(r[12:]), DbgStart: le.Uint32(r[16:]), DbgEnd: le.Uint32(r[20:]),
				Type: TypeIndex(le.Uint32(r[24:])), Offset: le.Uint32(r[28:]), Segment: le.Uint16(r[32:]),
				Flags: ProcFlags(r[34]), Name: cString(r[35:])}
		}
	case SPub32:
		if len(r) >= 10 {
			return &Public{Flags: PublicFlags(le.Uint32(r)), Offset: le.Uint32(r[4:]), Segment: le.Uint16(r[8:]), Name: cString(r[10:])}
		}
	case SGData32, SLData32, SGThread32, SLThread32:
		if len(r) >= 10 {
			return &Data{K: k, Type: TypeIndex(le.Uint32(r)), Offset: le.Uint32(r[4:]), Segment: le.Uint16(r[8:]), Name: cString(r[10:])}
		}
	case SRegRel32:
		if len(r) >= 10 {
			return &RegRel{Offset: int32(le.Uint32(r)), Type: TypeIndex(le.Uint32(r[4:])), Register: le.Uint16(r[8:]), Name: cString(r[10:])}
		}
	case SBPRel32:
		if len(r) >= 8 {
			return &BPRel{Offset: int32(le.Uint32(r)), Type: TypeIndex(le.Uint32(r[4:])), Name: cString(r[8:])}
		}
	case SLocal:
		if len(r) >= 6 {
			return &Local{Type: TypeIndex(le.Uint32(r)), Flags: LocalFlags(le.Uint16(r[4:])), Name: cString(r[6:])}
		}
	case SFrameProc:
		if len(r) >= 26 {
			return &FrameProc{FrameSize: le.Uint32(r), PadSize: le.Uint32(r[4:]), PadOffset: le.Uint32(r[8:]),
				SaveRegsSize: le.Uint32(r[12:]), Flags: le.Uint32(r[22:])}
		}
	case SUDT:
		if len(r) >= 4 {
			return &UDT{Type: TypeIndex(le.Uint32(r)), Name: cString(r[4:])}
		}
	case SProcRef, SLProcRef, SDataRef:
		if len(r) >= 10 {
			return &ProcRef{K: k, Offset: le.Uint32(r[4:]), Module: le.Uint16(r[8:]), Name: cString(r[10:])}
		}
	}
	return &RawSymbol{K: k, Data: r}
}

// ModuleSymbols decodes a module's symbol stream. Procedures carry their own
// locals; top-level records follow in stream order.
func (f *File) ModuleSymbols(m Module) ([]Symbol, error) {
	if m.SymbolStream < 0 || m.SymbolBytes <= 4 {
		return nil, nil
	}
	s, err := f.Stream(m.SymbolStream)
	if err != nil {
		return nil, err
	}
	if len(s) < int(m.SymbolBytes) {
		return nil, errors.New("pdb: module symbol stream shorter than its module info says")
	}
	// The stream starts with a 4-byte signature (CV_SIGNATURE_C13 = 4).
	return moduleSymbols(s[4:m.SymbolBytes], 4), nil
}

// moduleSymbols nests records under their procedure. A procedure's scope
// ends at its matching S_END (or S_PROC_ID_END); blocks, thunks and
// separated code nest inside it. Inline sites hold their inlinee's
// variables, so their contents are skipped.
func moduleSymbols(b []byte, base int) []Symbol {
	var out []Symbol
	var cur *Procedure
	depth, inline := 0, 0
	walk(b, base, func(off int, k SymbolKind, rec []byte) {
		switch k {
		case SGProc32, SLProc32, SGProc32ID, SLProc32ID:
			if cur == nil {
				sym := decodeSymbol(k, rec)
				if p, ok := sym.(*Procedure); ok {
					cur, depth, inline = p, 1, 0
					out = append(out, p)
					return
				}
				out = append(out, sym)
				return
			}
			depth++
		case SInlineSite, SInlineSite2:
			if cur != nil {
				inline++
				return
			}
		case SInlineSiteEnd:
			if cur != nil && inline > 0 {
				inline--
				return
			}
		case SBlock32, SThunk32, SSepCode:
			if cur != nil {
				depth++
			}
		case SEnd, SProcIDEnd:
			if cur != nil {
				depth--
				if depth == 0 {
					cur = nil
				}
				return
			}
		}
		if cur != nil {
			if inline == 0 {
				cur.Locals = append(cur.Locals, decodeSymbol(k, rec))
			}
			return
		}
		out = append(out, decodeSymbol(k, rec))
	})
	return out
}

// GlobalSymbols decodes the symbol record stream: publics, global and
// static data, procedure references and user-defined type names.
func (f *File) GlobalSymbols() ([]Symbol, error) {
	d, err := f.DBI()
	if err != nil {
		return nil, err
	}
	s, err := f.Stream(d.SymRecordStream)
	if err != nil {
		return nil, err
	}
	return Symbols(s), nil
}
