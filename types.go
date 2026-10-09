package pdb

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// TypeIndex identifies a type. Indices below TypeTable.Begin (0x1000) are
// built-in "simple" types (see Simple); the rest index TPI or IPI records.
type TypeIndex uint32

// Leaf is a CodeView type record kind (cvinfo.h LEAF_ENUM_e).
type Leaf uint16

const (
	LFVTShape    Leaf = 0x000a
	LFModifier   Leaf = 0x1001
	LFPointer    Leaf = 0x1002
	LFProcedure  Leaf = 0x1008
	LFMFunction  Leaf = 0x1009
	LFArgList    Leaf = 0x1201
	LFFieldList  Leaf = 0x1203
	LFBitfield   Leaf = 0x1205
	LFMethodList Leaf = 0x1206
	LFBClass     Leaf = 0x1400
	LFVBClass    Leaf = 0x1401
	LFIVBClass   Leaf = 0x1402
	LFIndex      Leaf = 0x1404
	LFVFuncTab   Leaf = 0x1409
	LFEnumerate  Leaf = 0x1502
	LFArray      Leaf = 0x1503
	LFClass      Leaf = 0x1504
	LFStructure  Leaf = 0x1505
	LFUnion      Leaf = 0x1506
	LFEnum       Leaf = 0x1507
	LFMember     Leaf = 0x150d
	LFSTMember   Leaf = 0x150e
	LFMethod     Leaf = 0x150f
	LFNestType   Leaf = 0x1510
	LFOneMethod  Leaf = 0x1511
	LFInterface  Leaf = 0x1519
	LFFuncID     Leaf = 0x1601
	LFMFuncID    Leaf = 0x1602
	LFBuildInfo  Leaf = 0x1603
	LFSubstrList Leaf = 0x1604
	LFStringID   Leaf = 0x1605
	LFUDTSrcLine Leaf = 0x1606
	LFUDTModSrc  Leaf = 0x1607
	LFClass2     Leaf = 0x1608
	LFStructure2 Leaf = 0x1609
	LFUnion2     Leaf = 0x160a
	LFInterface2 Leaf = 0x160b
	lfNumeric    Leaf = 0x8000
	lfChar       Leaf = 0x8000
	lfShort      Leaf = 0x8001
	lfUShort     Leaf = 0x8002
	lfLong       Leaf = 0x8003
	lfULong      Leaf = 0x8004
	lfReal32     Leaf = 0x8005
	lfReal64     Leaf = 0x8006
	lfQuadword   Leaf = 0x8009
	lfUQuadword  Leaf = 0x800a
	lfPadMinimum      = 0xf0
)

// Type is one decoded type record. Records this package does not decode are
// returned as *RawType.
type Type interface {
	Leaf() Leaf
}

// RawType is a record kept undecoded.
type RawType struct {
	L    Leaf
	Data []byte
}

func (t *RawType) Leaf() Leaf { return t.L }

// Modifier is LF_MODIFIER: a const/volatile/unaligned view of a type.
type Modifier struct {
	Type                       TypeIndex
	Const, Volatile, Unaligned bool
}

func (t *Modifier) Leaf() Leaf { return LFModifier }

// PointerMode is CV_ptrmode_e.
type PointerMode uint8

const (
	PtrModePointer    PointerMode = 0
	PtrModeLValueRef  PointerMode = 1
	PtrModeMemberData PointerMode = 2
	PtrModeMemberFunc PointerMode = 3
	PtrModeRValueRef  PointerMode = 4
)

// Pointer is LF_POINTER.
type Pointer struct {
	Referent                             TypeIndex
	Kind                                 uint8 // CV_ptrtype_e: 0x0a near32, 0x0c 64-bit
	Mode                                 PointerMode
	Size                                 uint8 // bytes; 0 when the record does not say
	Const, Volatile, Unaligned, Restrict bool
	// ContainingClass is the class of a pointer to member.
	ContainingClass TypeIndex
}

func (t *Pointer) Leaf() Leaf { return LFPointer }

// CallConv is CV_call_e.
type CallConv uint8

const (
	CallNearC      CallConv = 0x00 // __cdecl
	CallNearPascal CallConv = 0x02
	CallNearFast   CallConv = 0x04 // __fastcall
	CallNearStd    CallConv = 0x07 // __stdcall
	CallNearSys    CallConv = 0x09
	CallThis       CallConv = 0x0b // __thiscall
	CallClrCall    CallConv = 0x16
	CallNearVector CallConv = 0x18 // __vectorcall
)

// Procedure type options (CV_funcattr_t).
const (
	FuncCxxReturnUDT = 0x01
	FuncConstructor  = 0x02
	FuncCtorVBase    = 0x04
)

// ProcedureType is LF_PROCEDURE.
type ProcedureType struct {
	Return     TypeIndex
	CallConv   CallConv
	Options    uint8
	ParamCount uint16
	ArgList    TypeIndex
}

func (t *ProcedureType) Leaf() Leaf { return LFProcedure }

// MemberFunction is LF_MFUNCTION: a method type. This is the type of the
// implicit this pointer (0 for a static method).
type MemberFunction struct {
	Return, Class, This TypeIndex
	CallConv            CallConv
	Options             uint8
	ParamCount          uint16
	ArgList             TypeIndex
	ThisAdjust          int32
}

func (t *MemberFunction) Leaf() Leaf { return LFMFunction }

// ArgList is LF_ARGLIST. A trailing 0 (T_NOTYPE) argument marks varargs.
type ArgList struct {
	Args []TypeIndex
}

func (t *ArgList) Leaf() Leaf { return LFArgList }

// Class property bits (CV_prop_t).
const (
	PropPacked        uint16 = 0x0001
	PropFwdRef        uint16 = 0x0080
	PropScoped        uint16 = 0x0100
	PropHasUniqueName uint16 = 0x0200
)

// Class is LF_CLASS/LF_STRUCTURE/LF_INTERFACE (and the 0x16xx forms).
type Class struct {
	L          Leaf
	Count      uint16 // member count
	Properties uint16
	FieldList  TypeIndex
	Derived    TypeIndex
	VShape     TypeIndex
	Size       uint64
	Name       string
	UniqueName string // decorated name, when Properties has PropHasUniqueName
}

func (t *Class) Leaf() Leaf { return t.L }

// FwdRef reports a forward declaration; the definition is the record with
// the same unique name (or name) without this flag (see TypeTable.Resolve).
func (t *Class) FwdRef() bool { return t.Properties&PropFwdRef != 0 }

// Union is LF_UNION.
type Union struct {
	Count      uint16
	Properties uint16
	FieldList  TypeIndex
	Size       uint64
	Name       string
	UniqueName string
}

func (t *Union) Leaf() Leaf   { return LFUnion }
func (t *Union) FwdRef() bool { return t.Properties&PropFwdRef != 0 }

// Enum is LF_ENUM.
type Enum struct {
	Count      uint16
	Properties uint16
	Underlying TypeIndex
	FieldList  TypeIndex
	Name       string
	UniqueName string
}

func (t *Enum) Leaf() Leaf   { return LFEnum }
func (t *Enum) FwdRef() bool { return t.Properties&PropFwdRef != 0 }

// Array is LF_ARRAY. Size is the whole array's size in bytes.
type Array struct {
	Element, IndexType TypeIndex
	Size               uint64
	Name               string
}

func (t *Array) Leaf() Leaf { return LFArray }

// Bitfield is LF_BITFIELD.
type Bitfield struct {
	Type     TypeIndex
	Length   uint8
	Position uint8
}

func (t *Bitfield) Leaf() Leaf { return LFBitfield }

// VTShape is LF_VTSHAPE: the number of virtual table slots.
type VTShape struct {
	Count uint16
}

func (t *VTShape) Leaf() Leaf { return LFVTShape }

// FieldList is LF_FIELDLIST: the members of a class, union or enum. A long
// list continues in another record through an LF_INDEX member, which
// TypeTable.Fields follows.
type FieldList struct {
	Fields []Field
}

func (t *FieldList) Leaf() Leaf { return LFFieldList }

// Field is one member of a field list.
type Field interface {
	FieldLeaf() Leaf
}

// Member is LF_MEMBER: a non-static data member.
type Member struct {
	Attr   uint16
	Type   TypeIndex
	Offset uint64
	Name   string
}

func (f *Member) FieldLeaf() Leaf { return LFMember }

// StaticMember is LF_STMEMBER.
type StaticMember struct {
	Attr uint16
	Type TypeIndex
	Name string
}

func (f *StaticMember) FieldLeaf() Leaf { return LFSTMember }

// BaseClass is LF_BCLASS: a direct non-virtual base at Offset.
type BaseClass struct {
	Attr   uint16
	Type   TypeIndex
	Offset uint64
}

func (f *BaseClass) FieldLeaf() Leaf { return LFBClass }

// VirtualBaseClass is LF_VBCLASS/LF_IVBCLASS.
type VirtualBaseClass struct {
	L                       Leaf
	Attr                    uint16
	Type, VBPtrType         TypeIndex
	VBPtrOffset, VBTableIdx uint64
}

func (f *VirtualBaseClass) FieldLeaf() Leaf { return f.L }

// VFuncTab is LF_VFUNCTAB: the class has a virtual function table pointer.
type VFuncTab struct {
	Type TypeIndex
}

func (f *VFuncTab) FieldLeaf() Leaf { return LFVFuncTab }

// OneMethod is LF_ONEMETHOD: a single (non-overloaded) method.
type OneMethod struct {
	Attr        uint16
	Type        TypeIndex
	VBaseOffset uint32 // vtable offset of an introducing virtual method
	Name        string
}

func (f *OneMethod) FieldLeaf() Leaf { return LFOneMethod }

// Method is LF_METHOD: an overload set named by MethodList.
type Method struct {
	Count      uint16
	MethodList TypeIndex
	Name       string
}

func (f *Method) FieldLeaf() Leaf { return LFMethod }

// NestedType is LF_NESTTYPE.
type NestedType struct {
	Type TypeIndex
	Name string
}

func (f *NestedType) FieldLeaf() Leaf { return LFNestType }

// Enumerate is LF_ENUMERATE. Value is the bit pattern; Signed says whether
// it was encoded as a signed number.
type Enumerate struct {
	Attr   uint16
	Value  uint64
	Signed bool
	Name   string
}

func (f *Enumerate) FieldLeaf() Leaf { return LFEnumerate }

// IndexContinuation is LF_INDEX: the list continues in Type.
type IndexContinuation struct {
	Type TypeIndex
}

func (f *IndexContinuation) FieldLeaf() Leaf { return LFIndex }

// RawField is a member kind this package does not decode. Because members
// are not length-prefixed, decoding a field list stops after one.
type RawField struct {
	L    Leaf
	Data []byte
}

func (f *RawField) FieldLeaf() Leaf { return f.L }

// FuncID is LF_FUNC_ID (IPI): a global function's type and scope.
type FuncID struct {
	Scope TypeIndex // LF_STRING_ID of the namespace, or 0
	Type  TypeIndex // TPI LF_PROCEDURE
	Name  string
}

func (t *FuncID) Leaf() Leaf { return LFFuncID }

// MFuncID is LF_MFUNC_ID (IPI): a method's class and type.
type MFuncID struct {
	Parent TypeIndex // TPI class
	Type   TypeIndex // TPI LF_MFUNCTION
	Name   string
}

func (t *MFuncID) Leaf() Leaf { return LFMFuncID }

// MethodListEntry is one overload in an LF_METHODLIST.
type MethodListEntry struct {
	Attr        uint16
	Type        TypeIndex
	VBaseOffset uint32
}

// MethodList is LF_METHODLIST: the overloads an LF_METHOD names.
type MethodList struct {
	Methods []MethodListEntry
}

func (t *MethodList) Leaf() Leaf { return LFMethodList }

// BuildInfo is LF_BUILDINFO (IPI): string ids of the build directory,
// compiler, source file, PDB and command line.
type BuildInfo struct {
	Args []TypeIndex
}

func (t *BuildInfo) Leaf() Leaf { return LFBuildInfo }

// SubstrList is LF_SUBSTR_LIST (IPI): string ids joined into one string.
type SubstrList struct {
	Strings []TypeIndex
}

func (t *SubstrList) Leaf() Leaf { return LFSubstrList }

// UDTSourceLine is LF_UDT_SRC_LINE / LF_UDT_MOD_SRC_LINE (IPI): where a
// user-defined type is declared. Module is 0 for LF_UDT_SRC_LINE.
type UDTSourceLine struct {
	L          Leaf
	Type, File TypeIndex
	Line       uint32
	Module     uint16
}

func (t *UDTSourceLine) Leaf() Leaf { return t.L }

// StringID is LF_STRING_ID (IPI).
type StringID struct {
	SubstringList TypeIndex
	Value         string
}

func (t *StringID) Leaf() Leaf { return LFStringID }

// TypeTable is a TPI or IPI stream. Records are decoded on demand.
type TypeTable struct {
	Begin, End TypeIndex
	recs       [][]byte // record i (index Begin+i) after its length field
	defs       map[string]TypeIndex
}

// Types returns the TPI stream (cached).
func (f *File) Types() (*TypeTable, error) {
	if f.tpi == nil {
		t, err := f.typeTable(StreamTPI)
		if err != nil {
			return nil, err
		}
		f.tpi = t
	}
	return f.tpi, nil
}

// IDs returns the IPI stream (cached); nil when the PDB has none.
func (f *File) IDs() (*TypeTable, error) {
	if f.ipi == nil {
		if f.StreamSize(StreamIPI) == 0 {
			return nil, nil
		}
		t, err := f.typeTable(StreamIPI)
		if err != nil {
			return nil, err
		}
		f.ipi = t
	}
	return f.ipi, nil
}

func (f *File) typeTable(stream int) (*TypeTable, error) {
	s, err := f.Stream(stream)
	if err != nil {
		return nil, err
	}
	if len(s) < 56 {
		return nil, fmt.Errorf("pdb: type stream %d too short", stream)
	}
	le := binary.LittleEndian
	hdrSize := le.Uint32(s[4:])
	t := &TypeTable{Begin: TypeIndex(le.Uint32(s[8:])), End: TypeIndex(le.Uint32(s[12:]))}
	recBytes := le.Uint32(s[16:])
	if t.End < t.Begin || uint64(hdrSize)+uint64(recBytes) > uint64(len(s)) {
		return nil, fmt.Errorf("pdb: corrupt type stream %d header", stream)
	}
	b := s[hdrSize : hdrSize+recBytes]
	t.recs = make([][]byte, 0, t.End-t.Begin)
	for pos := 0; pos+4 <= len(b) && len(t.recs) < int(t.End-t.Begin); {
		n := int(le.Uint16(b[pos:]))
		if n < 2 || pos+2+n > len(b) {
			break
		}
		t.recs = append(t.recs, b[pos+2:pos+2+n])
		pos += 2 + n
	}
	return t, nil
}

// Len is the number of records.
func (t *TypeTable) Len() int { return len(t.recs) }

// Raw returns record ti's leaf and its bytes after the leaf field.
func (t *TypeTable) Raw(ti TypeIndex) (Leaf, []byte, bool) {
	if ti < t.Begin || int(ti-t.Begin) >= len(t.recs) {
		return 0, nil, false
	}
	r := t.recs[ti-t.Begin]
	if len(r) < 2 {
		return 0, nil, false
	}
	return Leaf(binary.LittleEndian.Uint16(r)), r[2:], true
}

// ErrNoType is returned for an index outside the table.
var ErrNoType = errors.New("pdb: type index out of range")

// Lookup decodes record ti.
func (t *TypeTable) Lookup(ti TypeIndex) (Type, error) {
	l, r, ok := t.Raw(ti)
	if !ok {
		return nil, ErrNoType
	}
	return decodeType(l, r)
}

// Fields returns the members of the field list fl, following LF_INDEX
// continuations.
func (t *TypeTable) Fields(fl TypeIndex) ([]Field, error) {
	var out []Field
	seen := map[TypeIndex]bool{}
	for fl != 0 && !seen[fl] {
		seen[fl] = true
		typ, err := t.Lookup(fl)
		if err != nil {
			return out, err
		}
		list, ok := typ.(*FieldList)
		if !ok {
			return out, fmt.Errorf("pdb: type 0x%x is not a field list", fl)
		}
		fl = 0
		for _, f := range list.Fields {
			if c, ok := f.(*IndexContinuation); ok {
				fl = c.Type
				continue
			}
			out = append(out, f)
		}
	}
	return out, nil
}

// Resolve returns the definition of a forward-declared class, union or
// enum (matched by unique name, else by name); other indices are returned
// unchanged.
func (t *TypeTable) Resolve(ti TypeIndex) TypeIndex {
	typ, err := t.Lookup(ti)
	if err != nil {
		return ti
	}
	var key string
	switch c := typ.(type) {
	case *Class:
		if !c.FwdRef() {
			return ti
		}
		key = udtKey(c.Leaf(), c.Name, c.UniqueName)
	case *Union:
		if !c.FwdRef() {
			return ti
		}
		key = udtKey(LFUnion, c.Name, c.UniqueName)
	case *Enum:
		if !c.FwdRef() {
			return ti
		}
		key = udtKey(LFEnum, c.Name, c.UniqueName)
	default:
		return ti
	}
	if t.defs == nil {
		t.indexDefinitions()
	}
	if d, ok := t.defs[key]; ok {
		return d
	}
	return ti
}

// ByName finds the definition (not a forward declaration) of the class,
// structure, union or enumeration with the given qualified name.
func (t *TypeTable) ByName(name string) (TypeIndex, bool) {
	if t.defs == nil {
		t.indexDefinitions()
	}
	for _, l := range []Leaf{LFClass, LFUnion, LFEnum} {
		if ti, ok := t.defs[udtKey(l, name, "")]; ok {
			return ti, true
		}
	}
	return 0, false
}

func udtKey(l Leaf, name, unique string) string {
	// Classes, structures and interfaces share one namespace.
	switch l {
	case LFStructure, LFInterface, LFClass2, LFStructure2, LFInterface2:
		l = LFClass
	case LFUnion2:
		l = LFUnion
	}
	if unique != "" {
		return fmt.Sprintf("%x:u:%s", uint16(l), unique)
	}
	return fmt.Sprintf("%x:n:%s", uint16(l), name)
}

func (t *TypeTable) indexDefinitions() {
	t.defs = map[string]TypeIndex{}
	add := func(l Leaf, name, unique string, ti TypeIndex) {
		if unique != "" {
			if _, ok := t.defs[udtKey(l, "", unique)]; !ok {
				t.defs[udtKey(l, "", unique)] = ti
			}
		}
		if _, ok := t.defs[udtKey(l, name, "")]; !ok {
			t.defs[udtKey(l, name, "")] = ti
		}
	}
	for i := range t.recs {
		ti := t.Begin + TypeIndex(i)
		l, _, _ := t.Raw(ti)
		switch l {
		case LFClass, LFStructure, LFInterface, LFUnion, LFEnum, LFClass2, LFStructure2, LFUnion2, LFInterface2:
		default:
			continue
		}
		typ, err := t.Lookup(ti)
		if err != nil {
			continue
		}
		switch c := typ.(type) {
		case *Class:
			if !c.FwdRef() {
				add(c.Leaf(), c.Name, c.UniqueName, ti)
			}
		case *Union:
			if !c.FwdRef() {
				add(LFUnion, c.Name, c.UniqueName, ti)
			}
		case *Enum:
			if !c.FwdRef() {
				add(LFEnum, c.Name, c.UniqueName, ti)
			}
		}
	}
}

// reader decodes the fields of one record.
type reader struct {
	b   []byte
	err error
}

func (r *reader) need(n int) bool {
	if r.err == nil && len(r.b) < n {
		r.err = errors.New("pdb: truncated type record")
	}
	return r.err == nil
}

func (r *reader) u8() uint8 {
	if !r.need(1) {
		return 0
	}
	v := r.b[0]
	r.b = r.b[1:]
	return v
}

func (r *reader) u16() uint16 {
	if !r.need(2) {
		return 0
	}
	v := binary.LittleEndian.Uint16(r.b)
	r.b = r.b[2:]
	return v
}

func (r *reader) u32() uint32 {
	if !r.need(4) {
		return 0
	}
	v := binary.LittleEndian.Uint32(r.b)
	r.b = r.b[4:]
	return v
}

func (r *reader) ti() TypeIndex { return TypeIndex(r.u32()) }

// numeric reads a CodeView numeric leaf: a literal below 0x8000, else a
// typed value. signed reports a signed encoding.
func (r *reader) numeric() (v uint64, signed bool) {
	k := r.u16()
	if r.err != nil {
		return 0, false
	}
	if k < uint16(lfNumeric) {
		return uint64(k), false
	}
	switch Leaf(k) {
	case lfChar:
		return uint64(int64(int8(r.u8()))), true
	case lfShort:
		return uint64(int64(int16(r.u16()))), true
	case lfUShort:
		return uint64(r.u16()), false
	case lfLong:
		return uint64(int64(int32(r.u32()))), true
	case lfULong:
		return uint64(r.u32()), false
	case lfQuadword, lfUQuadword:
		if !r.need(8) {
			return 0, false
		}
		v := binary.LittleEndian.Uint64(r.b)
		r.b = r.b[8:]
		return v, Leaf(k) == lfQuadword
	case lfReal32:
		return uint64(r.u32()), false
	case lfReal64:
		if r.need(8) {
			v := binary.LittleEndian.Uint64(r.b)
			r.b = r.b[8:]
			return v, false
		}
	default:
		r.err = fmt.Errorf("pdb: unsupported numeric leaf 0x%x", k)
	}
	return 0, false
}

func (r *reader) str() string {
	if r.err != nil {
		return ""
	}
	for i, c := range r.b {
		if c == 0 {
			s := string(r.b[:i])
			r.b = r.b[i+1:]
			return s
		}
	}
	s := string(r.b)
	r.b = nil
	return s
}

// skipPad drops LF_PAD bytes between field list members.
func (r *reader) skipPad() {
	for len(r.b) > 0 && r.b[0] >= lfPadMinimum {
		n := int(r.b[0] & 0x0f)
		if n == 0 || n > len(r.b) {
			n = 1
		}
		r.b = r.b[n:]
	}
}

func decodeType(l Leaf, b []byte) (Type, error) {
	r := &reader{b: b}
	var t Type
	switch l {
	case LFModifier:
		m := &Modifier{Type: r.ti()}
		a := r.u16()
		m.Const, m.Volatile, m.Unaligned = a&1 != 0, a&2 != 0, a&4 != 0
		t = m
	case LFPointer:
		p := &Pointer{Referent: r.ti()}
		a := r.u32()
		p.Kind = uint8(a & 0x1f)
		p.Mode = PointerMode(a >> 5 & 0x7)
		p.Volatile, p.Const, p.Unaligned, p.Restrict = a&(1<<9) != 0, a&(1<<10) != 0, a&(1<<11) != 0, a&(1<<12) != 0
		p.Size = uint8(a >> 13 & 0x3f)
		if p.Mode == PtrModeMemberData || p.Mode == PtrModeMemberFunc {
			p.ContainingClass = r.ti()
		}
		t = p
	case LFProcedure:
		t = &ProcedureType{Return: r.ti(), CallConv: CallConv(r.u8()), Options: r.u8(), ParamCount: r.u16(), ArgList: r.ti()}
	case LFMFunction:
		t = &MemberFunction{Return: r.ti(), Class: r.ti(), This: r.ti(), CallConv: CallConv(r.u8()), Options: r.u8(),
			ParamCount: r.u16(), ArgList: r.ti(), ThisAdjust: int32(r.u32())}
	case LFArgList:
		n := r.u32()
		if uint64(n)*4 > uint64(len(r.b)) {
			return nil, errors.New("pdb: truncated argument list")
		}
		a := &ArgList{Args: make([]TypeIndex, n)}
		for i := range a.Args {
			a.Args[i] = r.ti()
		}
		t = a
	case LFClass, LFStructure, LFInterface:
		c := &Class{L: l, Count: r.u16(), Properties: r.u16(), FieldList: r.ti(), Derived: r.ti(), VShape: r.ti()}
		c.Size, _ = r.numeric()
		c.Name = r.str()
		if c.Properties&PropHasUniqueName != 0 {
			c.UniqueName = r.str()
		}
		t = c
	case LFClass2, LFStructure2, LFInterface2:
		// VS2019+ form: the property field widened to 32 bits, count moved.
		props := r.u32()
		c := &Class{L: l, Properties: uint16(props), FieldList: r.ti(), Derived: r.ti(), VShape: r.ti()}
		c.Count = r.u16()
		c.Size, _ = r.numeric()
		c.Name = r.str()
		if c.Properties&PropHasUniqueName != 0 {
			c.UniqueName = r.str()
		}
		t = c
	case LFUnion:
		u := &Union{Count: r.u16(), Properties: r.u16(), FieldList: r.ti()}
		u.Size, _ = r.numeric()
		u.Name = r.str()
		if u.Properties&PropHasUniqueName != 0 {
			u.UniqueName = r.str()
		}
		t = u
	case LFUnion2:
		props := r.u32()
		u := &Union{Properties: uint16(props), FieldList: r.ti()}
		u.Count = r.u16()
		u.Size, _ = r.numeric()
		u.Name = r.str()
		if u.Properties&PropHasUniqueName != 0 {
			u.UniqueName = r.str()
		}
		t = u
	case LFEnum:
		e := &Enum{Count: r.u16(), Properties: r.u16(), Underlying: r.ti(), FieldList: r.ti()}
		e.Name = r.str()
		if e.Properties&PropHasUniqueName != 0 {
			e.UniqueName = r.str()
		}
		t = e
	case LFArray:
		a := &Array{Element: r.ti(), IndexType: r.ti()}
		a.Size, _ = r.numeric()
		a.Name = r.str()
		t = a
	case LFBitfield:
		t = &Bitfield{Type: r.ti(), Length: r.u8(), Position: r.u8()}
	case LFVTShape:
		t = &VTShape{Count: r.u16()}
	case LFFieldList:
		fl := &FieldList{}
		for len(r.b) > 0 && r.err == nil {
			f := decodeField(r)
			if f == nil {
				break
			}
			fl.Fields = append(fl.Fields, f)
			if _, raw := f.(*RawField); raw {
				break // unknown member length: cannot continue
			}
			r.skipPad()
		}
		t = fl
	case LFFuncID:
		t = &FuncID{Scope: r.ti(), Type: r.ti(), Name: r.str()}
	case LFMFuncID:
		t = &MFuncID{Parent: r.ti(), Type: r.ti(), Name: r.str()}
	case LFStringID:
		t = &StringID{SubstringList: r.ti(), Value: r.str()}
	case LFMethodList:
		ml := &MethodList{}
		for len(r.b) >= 8 && r.err == nil {
			e := MethodListEntry{Attr: r.u16()}
			r.u16() // padding
			e.Type = r.ti()
			if mp := e.Attr >> 2 & 0x7; mp == 4 || mp == 6 {
				e.VBaseOffset = r.u32()
			}
			ml.Methods = append(ml.Methods, e)
		}
		t = ml
	case LFBuildInfo:
		n := int(r.u16())
		bi := &BuildInfo{}
		for i := 0; i < n && r.err == nil; i++ {
			bi.Args = append(bi.Args, r.ti())
		}
		t = bi
	case LFSubstrList:
		n := r.u32()
		if uint64(n)*4 > uint64(len(r.b)) {
			return nil, errors.New("pdb: truncated substring list")
		}
		sl := &SubstrList{Strings: make([]TypeIndex, n)}
		for i := range sl.Strings {
			sl.Strings[i] = r.ti()
		}
		t = sl
	case LFUDTSrcLine:
		t = &UDTSourceLine{L: l, Type: r.ti(), File: r.ti(), Line: r.u32()}
	case LFUDTModSrc:
		t = &UDTSourceLine{L: l, Type: r.ti(), File: r.ti(), Line: r.u32(), Module: r.u16()}
	default:
		return &RawType{L: l, Data: b}, nil
	}
	if r.err != nil {
		return nil, fmt.Errorf("%w (leaf 0x%x)", r.err, uint16(l))
	}
	return t, nil
}

func decodeField(r *reader) Field {
	l := Leaf(r.u16())
	if r.err != nil {
		return nil
	}
	switch l {
	case LFMember:
		m := &Member{Attr: r.u16(), Type: r.ti()}
		m.Offset, _ = r.numeric()
		m.Name = r.str()
		return m
	case LFSTMember:
		return &StaticMember{Attr: r.u16(), Type: r.ti(), Name: r.str()}
	case LFBClass:
		b := &BaseClass{Attr: r.u16(), Type: r.ti()}
		b.Offset, _ = r.numeric()
		return b
	case LFVBClass, LFIVBClass:
		v := &VirtualBaseClass{L: l, Attr: r.u16(), Type: r.ti(), VBPtrType: r.ti()}
		v.VBPtrOffset, _ = r.numeric()
		v.VBTableIdx, _ = r.numeric()
		return v
	case LFVFuncTab:
		r.u16()
		return &VFuncTab{Type: r.ti()}
	case LFOneMethod:
		m := &OneMethod{Attr: r.u16(), Type: r.ti()}
		// An introducing virtual method (mprop intro or pure intro) carries
		// its vtable offset.
		if mp := m.Attr >> 2 & 0x7; mp == 4 || mp == 6 {
			m.VBaseOffset = r.u32()
		}
		m.Name = r.str()
		return m
	case LFMethod:
		return &Method{Count: r.u16(), MethodList: r.ti(), Name: r.str()}
	case LFNestType:
		r.u16()
		return &NestedType{Type: r.ti(), Name: r.str()}
	case LFEnumerate:
		e := &Enumerate{Attr: r.u16()}
		e.Value, e.Signed = r.numeric()
		e.Name = r.str()
		return e
	case LFIndex:
		r.u16()
		return &IndexContinuation{Type: r.ti()}
	}
	f := &RawField{L: l, Data: r.b}
	r.b = nil
	return f
}
