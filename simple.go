package pdb

// Simple types: indices below 0x1000 encode a built-in type and a pointer
// mode instead of naming a record. Bits 0-7 are the kind (TYPE_ENUM_e),
// bits 8-10 the mode (CV_prmode_e).

// SimpleClass groups simple kinds by how a consumer represents them.
type SimpleClass uint8

const (
	SimpleNone     SimpleClass = iota // T_NOTYPE / unknown kind
	SimpleVoid                        // void
	SimpleSigned                      // signed integer
	SimpleUnsigned                    // unsigned integer
	SimpleChar                        // narrow character
	SimpleWideChar                    // wchar_t, char16_t, char32_t
	SimpleFloat                       // floating point
	SimpleBool                        // bool
	SimpleHResult                     // HRESULT
)

// Pointer modes of a simple type index.
const (
	SimpleDirect   = 0 // the type itself
	SimpleNear32   = 4 // 32-bit pointer to it
	SimpleNear64   = 6 // 64-bit pointer to it
	simpleModeMask = 0x7
)

type simpleInfo struct {
	name  string
	size  int
	class SimpleClass
}

var simpleKinds = map[uint8]simpleInfo{
	0x03: {"void", 0, SimpleVoid},
	0x08: {"HRESULT", 4, SimpleHResult},
	0x10: {"signed char", 1, SimpleChar},
	0x20: {"unsigned char", 1, SimpleChar},
	0x70: {"char", 1, SimpleChar},
	0x7c: {"char8_t", 1, SimpleChar},
	0x71: {"wchar_t", 2, SimpleWideChar},
	0x7a: {"char16_t", 2, SimpleWideChar},
	0x7b: {"char32_t", 4, SimpleWideChar},
	0x68: {"__int8", 1, SimpleSigned},
	0x69: {"unsigned __int8", 1, SimpleUnsigned},
	0x11: {"short", 2, SimpleSigned},
	0x21: {"unsigned short", 2, SimpleUnsigned},
	0x72: {"__int16", 2, SimpleSigned},
	0x73: {"unsigned __int16", 2, SimpleUnsigned},
	0x12: {"long", 4, SimpleSigned},
	0x22: {"unsigned long", 4, SimpleUnsigned},
	0x74: {"int", 4, SimpleSigned},
	0x75: {"unsigned int", 4, SimpleUnsigned},
	0x13: {"__int64", 8, SimpleSigned},
	0x23: {"unsigned __int64", 8, SimpleUnsigned},
	0x76: {"__int64", 8, SimpleSigned},
	0x77: {"unsigned __int64", 8, SimpleUnsigned},
	0x14: {"__int128", 16, SimpleSigned},
	0x24: {"unsigned __int128", 16, SimpleUnsigned},
	0x78: {"__int128", 16, SimpleSigned},
	0x79: {"unsigned __int128", 16, SimpleUnsigned},
	0x46: {"__half", 2, SimpleFloat},
	0x40: {"float", 4, SimpleFloat},
	0x41: {"double", 8, SimpleFloat},
	0x42: {"long double", 10, SimpleFloat},
	0x43: {"__float128", 16, SimpleFloat},
	0x30: {"bool", 1, SimpleBool},
	0x31: {"__bool16", 2, SimpleBool},
	0x32: {"__bool32", 4, SimpleBool},
	0x33: {"__bool64", 8, SimpleBool},
}

// SimpleType describes a simple type index.
type SimpleType struct {
	Name  string // C spelling of the kind, e.g. "unsigned int"
	Size  int    // size of the kind in bytes (0 for void)
	Class SimpleClass
	// Mode is the pointer mode: SimpleDirect, or a pointer to the kind
	// (SimpleNear32, SimpleNear64, ...).
	Mode uint8
}

// PointerSize is the byte size of a pointer of this mode, 0 when direct.
func (s SimpleType) PointerSize() int {
	switch s.Mode {
	case SimpleDirect:
		return 0
	case 1:
		return 2
	case 2, 3, SimpleNear32, 5:
		return 4
	case SimpleNear64:
		return 8
	default:
		return 16
	}
}

// IsSimple reports whether ti is a built-in type rather than a record.
func IsSimple(ti TypeIndex) bool { return ti < 0x1000 }

// Simple decodes a simple type index. ok is false for a record index or an
// unknown kind.
func Simple(ti TypeIndex) (SimpleType, bool) {
	if !IsSimple(ti) {
		return SimpleType{}, false
	}
	info, ok := simpleKinds[uint8(ti)]
	if !ok {
		return SimpleType{Mode: uint8(ti>>8) & simpleModeMask}, false
	}
	return SimpleType{Name: info.name, Size: info.size, Class: info.class, Mode: uint8(ti>>8) & simpleModeMask}, true
}
