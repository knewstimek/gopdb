// Package demangle decodes Microsoft Visual C++ decorated names
// ("?loadTailoring@CollationLoader@icu_53@@SAPEBU...") into their structure:
// the qualified name, and for functions the calling convention, return and
// parameter types; for variables the type.
//
// Symbol.String renders the result the way undname.exe does, which is how
// the package is tested. Encodings it does not understand (RTTI descriptors,
// some thunks, uncommon template arguments) return an error rather than a
// guess.
package demangle

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Kind is what a decorated name denotes.
type Kind int

const (
	KindUnknown  Kind = iota
	KindFunction      // a function or method
	KindData          // a variable or static member
	KindSpecial       // compiler-generated data: vftable, vbtable, local static guard
)

// Symbol is a demangled name.
type Symbol struct {
	Kind Kind
	// Name is the qualified name, outermost scope first:
	// ["icu_53", "CollationLoader", "loadTailoring"]. Constructors,
	// destructors and operators carry their C++ spelling ("Locale",
	// "~Locale", "operator=="); compiler-generated names their quoted form
	// ("`vftable'", "`scalar deleting destructor'").
	Name []string
	Func *Function // KindFunction
	Type *Type     // KindData
	// Access is "public", "protected", "private" or "" (namespace scope).
	Access string
	// Storage of a variable: "static" for static members and globals marked
	// so by undname; empty otherwise.
	Storage string
	// For KindSpecial: the qualifier undname prints after the name
	// ("{for `X'}") and the cv qualifiers ("const").
	SpecialFor string
	Quals      string
}

// Function is a function or method signature.
type Function struct {
	CallConv string // "__cdecl", "__stdcall", "__thiscall", "__fastcall", "__vectorcall", ...
	Return   *Type  // nil for constructors and destructors
	Params   []*Type
	Variadic bool // trailing "..."
	// Member functions: Static and Virtual, and the qualifiers of the
	// implicit this ("const", "__ptr64").
	Member, Static, Virtual bool
	ThisQuals               string
	// Adjustor is the this adjustment of a thunk (`adjustor{N}').
	Adjustor int64
	Thunk    bool
	// Conversion marks a conversion operator: Return is the target type
	// and is part of the name ("operator int").
	Conversion bool
}

// TypeKind classifies a Type.
type TypeKind int

const (
	TypePrimitive TypeKind = iota
	TypePointer
	TypeReference
	TypeRValueReference
	TypeNamed    // class, struct, union, enum
	TypeArray    // only as a pointee
	TypeFunction // only as a pointee or template argument
	TypeMemberPointer
	TypeNullptr
	TypeVarargs // the "..." of a function type in a template argument
)

// Type is a decoded type.
type Type struct {
	Kind TypeKind
	// Name is the primitive spelling ("unsigned int", "__int64") or the
	// qualified name of a named type, scopes joined by "::".
	Name string
	// Tag is "class", "struct", "union" or "enum" for a named type.
	Tag string
	// Underlying is an enum's underlying type when given ("int").
	Underlying string
	Const      bool
	Volatile   bool
	// Ptr64 marks a 64-bit pointer or reference (__ptr64); Restrict,
	// Unaligned are its other qualifiers.
	Ptr64, Restrict, Unaligned bool
	// PointerConst/Volatile qualify the pointer itself (P vs Q/R/S).
	PointerConst, PointerVolatile bool
	Pointee                       *Type
	Func                          *Function // TypeFunction, and a function pointer's pointee
	Dims                          []int64   // TypeArray
	Class                         string    // TypeMemberPointer: the class

	storagePtr64 bool // a pointer variable's own __ptr64 (rendering only)
}

// Demangle decodes a decorated name.
func Demangle(mangled string) (*Symbol, error) {
	if !strings.HasPrefix(mangled, "?") {
		return nil, errors.New("demangle: not a Microsoft decorated name")
	}
	d := &demangler{s: mangled, pos: 1}
	sym, err := d.symbol()
	if err != nil {
		return nil, fmt.Errorf("demangle %q: %w", mangled, err)
	}
	return sym, nil
}

var errBad = errors.New("malformed decoration")

type demangler struct {
	s     string
	pos   int
	names []string // name back-references 0-9
	types []*Type  // parameter back-references 0-9
	depth int
}

func (d *demangler) peek() byte {
	if d.pos < len(d.s) {
		return d.s[d.pos]
	}
	return 0
}

func (d *demangler) next() byte {
	c := d.peek()
	if c != 0 {
		d.pos++
	}
	return c
}

func (d *demangler) consume(p string) bool {
	if strings.HasPrefix(d.s[d.pos:], p) {
		d.pos += len(p)
		return true
	}
	return false
}

func (d *demangler) rememberName(n string) {
	if len(d.names) < 10 {
		for _, x := range d.names {
			if x == n {
				return
			}
		}
		d.names = append(d.names, n)
	}
}

// symbol parses after the leading '?'.
func (d *demangler) symbol() (*Symbol, error) {
	d.depth++
	defer func() { d.depth-- }()
	if d.depth > 20 {
		return nil, errors.New("nesting too deep")
	}
	if sym, ok, err := d.dataSpecial(); ok || err != nil {
		return sym, err
	}
	// Special data names: ??_7 vftable, ??_8 vbtable.
	if strings.HasPrefix(d.s[d.pos:], "?_7") || strings.HasPrefix(d.s[d.pos:], "?_8") {
		special := "`vftable'"
		if d.s[d.pos+2] == '8' {
			special = "`vbtable'"
		}
		d.pos += 3
		scope, err := d.scope()
		if err != nil {
			return nil, err
		}
		sym := &Symbol{Kind: KindSpecial, Name: append(scope, special)}
		if !d.consume("6") {
			return nil, errBad
		}
		sym.Quals = d.cvName()
		if d.peek() != '@' {
			// {for `Base'} -- the subobject the table belongs to.
			for d.peek() != '@' && d.peek() != 0 {
				base, err := d.scope()
				if err != nil {
					return nil, err
				}
				sym.SpecialFor = strings.Join(base, "::")
			}
		}
		d.consume("@")
		return sym, nil
	}

	first, kind, err := d.unqualifiedName(true, false)
	if err != nil {
		return nil, err
	}
	scope, err := d.scope()
	if err != nil {
		return nil, err
	}
	name := append(scope, first)
	switch kind {
	case nameCtor:
		if len(scope) == 0 {
			return nil, errBad
		}
		name[len(name)-1] = scope[len(scope)-1]
	case nameDtor:
		if len(scope) == 0 {
			return nil, errBad
		}
		name[len(name)-1] = "~" + scope[len(scope)-1]
	}
	sym := &Symbol{Name: name}

	c := d.peek()
	switch {
	case c >= '0' && c <= '4':
		d.next()
		sym.Kind = KindData
		switch c {
		case '0':
			sym.Access, sym.Storage = "private", "static"
		case '1':
			sym.Access, sym.Storage = "protected", "static"
		case '2':
			sym.Access, sym.Storage = "public", "static"
		}
		t, err := d.typ(false)
		if err != nil {
			return nil, err
		}
		ptr64, q, err := d.storageQuals()
		if err != nil {
			return nil, err
		}
		// The variable's own __ptr64 qualifies a pointer variable once more.
		if ptr64 && (t.Kind == TypePointer || t.Kind == TypeReference) {
			t.storagePtr64 = true
		}
		applyQuals(t, q)
		sym.Type = t
		return sym, nil
	case c == '_' || c == '$' || c >= 'A' && c <= 'Z':
		f, access, err := d.function()
		if err != nil {
			return nil, err
		}
		sym.Kind, sym.Func, sym.Access = KindFunction, f, access
		if kind == nameConversion && f.Return != nil {
			name[len(name)-1] = "operator " + f.Return.String()
			f.Conversion = true
		}
		return sym, nil
	}
	return nil, fmt.Errorf("unsupported encoding %q", c)
}

type nameKind int

const (
	namePlain nameKind = iota
	nameCtor
	nameDtor
	nameConversion
	nameOperator
)

var operators = map[string]string{
	"2": "operator new", "3": "operator delete", "4": "operator=", "5": "operator>>", "6": "operator<<",
	"7": "operator!", "8": "operator==", "9": "operator!=", "A": "operator[]", "C": "operator->",
	"D": "operator*", "E": "operator++", "F": "operator--", "G": "operator-", "H": "operator+",
	"I": "operator&", "J": "operator->*", "K": "operator/", "L": "operator%", "M": "operator<",
	"N": "operator<=", "O": "operator>", "P": "operator>=", "Q": "operator,", "R": "operator()",
	"S": "operator~", "T": "operator^", "U": "operator|", "V": "operator&&", "W": "operator||",
	"X": "operator*=", "Y": "operator+=", "Z": "operator-=",
	"_0": "operator/=", "_1": "operator%=", "_2": "operator>>=", "_3": "operator<<=", "_4": "operator&=",
	"_5": "operator|=", "_6": "operator^=",
	"_D": "`vbase destructor'", "_E": "`vector deleting destructor'", "_F": "`default constructor closure'",
	"_G": "`scalar deleting destructor'", "_H": "`vector constructor iterator'",
	"_I": "`vector destructor iterator'", "_J": "`vector vbase constructor iterator'",
	"_K": "`virtual displacement map'", "_L": "`eh vector constructor iterator'",
	"_M": "`eh vector destructor iterator'", "_N": "`eh vector vbase constructor iterator'",
	"_O": "`copy constructor closure'", "_S": "`local vftable'", "_T": "`local vftable constructor closure'",
	"_U": "operator new[]", "_V": "operator delete[]", "_X": "`placement delete closure'",
	"_Y":  "`placement delete[] closure'",
	"__L": "operator co_await", "__M": "operator<=>",
}

// dataSpecial decodes compiler-generated data: string literals (??_C) and
// RTTI descriptors (??_R0 .. ??_R4).
func (d *demangler) dataSpecial() (*Symbol, bool, error) {
	rest := d.s[d.pos:]
	switch {
	case strings.HasPrefix(rest, "?_C@"):
		d.pos = len(d.s)
		return &Symbol{Kind: KindSpecial, Name: []string{"`string'"}}, true, nil
	case strings.HasPrefix(rest, "?_R0"):
		// ??_R0 + type (with a ?A storage prefix) + @8
		d.pos += 4
		if !d.consume("?") {
			return nil, true, errBad
		}
		d.cvName()
		t, err := d.typ(false)
		if err != nil {
			return nil, true, err
		}
		return &Symbol{Kind: KindSpecial, Name: []string{t.String() + " `RTTI Type Descriptor'"}}, true, nil
	case strings.HasPrefix(rest, "?_R1"):
		d.pos += 4
		var n [4]int64
		for i := range n {
			v, err := d.number()
			if err != nil {
				return nil, true, err
			}
			n[i] = v
		}
		cls, err := d.qualifiedTypeName()
		if err != nil {
			return nil, true, err
		}
		name := fmt.Sprintf("`RTTI Base Class Descriptor at (%d,%d,%d,%d)'", n[0], n[1], n[2], n[3])
		return &Symbol{Kind: KindSpecial, Name: append(strings.Split(cls, "::"), name)}, true, nil
	case strings.HasPrefix(rest, "?_R2"), strings.HasPrefix(rest, "?_R3"), strings.HasPrefix(rest, "?_R4"):
		which := map[byte]string{'2': "`RTTI Base Class Array'", '3': "`RTTI Class Hierarchy Descriptor'",
			'4': "`RTTI Complete Object Locator'"}[rest[3]]
		d.pos += 4
		scope, err := d.scope()
		if err != nil {
			return nil, true, err
		}
		sym := &Symbol{Kind: KindSpecial, Name: append(scope, which)}
		if rest[3] == '4' && d.consume("6") {
			sym.Quals = d.cvName()
			if d.peek() != '@' && d.peek() != 0 {
				base, err := d.scope()
				if err != nil {
					return nil, true, err
				}
				sym.SpecialFor = strings.Join(base, "::")
			}
		}
		return sym, true, nil
	}
	return nil, false, nil
}

// unqualifiedName reads the innermost name. top marks the symbol's own name
// (where "?" introduces an operator). memoTemplate says whether a template
// instantiation is remembered for back-references: for scopes and type
// names, not for the symbol's own name (LLVM NBB_Template vs NBB_Simple).
func (d *demangler) unqualifiedName(top, memoTemplate bool) (string, nameKind, error) {
	c := d.peek()
	switch {
	case c >= '0' && c <= '9':
		d.next()
		i := int(c - '0')
		if i >= len(d.names) {
			return "", namePlain, errors.New("name back-reference out of range")
		}
		return d.names[i], namePlain, nil
	case strings.HasPrefix(d.s[d.pos:], "?$"):
		n, err := d.templateName()
		if err != nil {
			return "", namePlain, err
		}
		if memoTemplate {
			d.rememberName(n)
		}
		return n, namePlain, nil
	case c == '?' && top:
		d.next()
		return d.operatorName()
	}
	n, err := d.simpleName()
	if err != nil {
		return "", namePlain, err
	}
	d.rememberName(n)
	return n, namePlain, nil
}

func (d *demangler) simpleName() (string, error) {
	i := strings.IndexByte(d.s[d.pos:], '@')
	if i <= 0 {
		return "", errBad
	}
	n := d.s[d.pos : d.pos+i]
	d.pos += i + 1
	return n, nil
}

func (d *demangler) operatorName() (string, nameKind, error) {
	switch {
	case d.consume("0"):
		return "", nameCtor, nil
	case d.consume("1"):
		return "", nameDtor, nil
	case d.consume("B"):
		return "", nameConversion, nil
	case d.consume("__E"), d.consume("__F"):
		// `dynamic initializer for 'x'' / `dynamic atexit destructor for 'x''
		which := "`dynamic initializer for '"
		if d.s[d.pos-1] == 'F' {
			which = "`dynamic atexit destructor for '"
		}
		var inner string
		if d.peek() == '?' {
			d.next()
			sub := &demangler{s: d.s, pos: d.pos, depth: d.depth}
			sym, err := sub.symbol()
			if err != nil {
				return "", namePlain, err
			}
			d.pos = sub.pos
			inner = strings.Join(sym.Name, "::")
		} else {
			n, err := d.simpleName()
			if err != nil {
				return "", namePlain, err
			}
			inner = n
		}
		d.consume("@")
		return which + inner + "''", nameOperator, nil
	}
	for _, l := range []int{3, 2, 1} {
		if d.pos+l <= len(d.s) {
			if op, ok := operators[d.s[d.pos:d.pos+l]]; ok {
				d.pos += l
				return op, nameOperator, nil
			}
		}
	}
	return "", namePlain, errors.New("unsupported special name")
}

// templateName reads "?$name@args@". Template arguments use their own
// back-reference tables.
func (d *demangler) templateName() (string, error) {
	d.pos += 2
	saveNames, saveTypes := d.names, d.types
	d.names, d.types = nil, nil
	defer func() { d.names, d.types = saveNames, saveTypes }()
	// A template's name may be an operator: ??$?R$0BLI@...@ is
	// operator()<440,...>.
	base, kind, err := d.unqualifiedName(true, false)
	if err != nil {
		return "", err
	}
	if kind != namePlain && kind != nameOperator {
		return "", errors.New("unsupported template name")
	}
	var args []string
	for d.peek() != '@' {
		if d.peek() == 0 {
			return "", errBad
		}
		a, cvLast, err := d.templateArg()
		if err != nil {
			return "", err
		}
		if a != "" {
			// undname leaves a space after a type's own trailing cv
			// qualifier ("class X const ,0"), not after a const pointer.
			if cvLast {
				a += " "
			}
			args = append(args, a)
		}
	}
	d.next()
	s := base + "<" + strings.Join(args, ",")
	if strings.HasSuffix(s, ">") {
		s += " "
	}
	return s + ">", nil
}

// templateArg reads one template argument; cvLast reports that it ends in
// the type's own cv qualifier.
func (d *demangler) templateArg() (arg string, cvLast bool, err error) {
	switch {
	case d.consume("$0"):
		n, err := d.number()
		return strconv.FormatInt(n, 10), false, err
	case d.consume("$1"):
		if d.consume("?") {
			sub := &demangler{s: d.s, pos: d.pos, depth: d.depth}
			sym, err := sub.symbol()
			if err != nil {
				return "", false, err
			}
			d.pos = sub.pos
			return "&" + sym.String(), false, nil
		}
		return "", false, errors.New("unsupported template address argument")
	case d.consume("$S"), d.consume("$$V"), d.consume("$$Z"):
		return "", false, nil // empty parameter pack
	case d.consume("$$T"):
		return "std::nullptr_t", false, nil
	}
	if strings.HasPrefix(d.s[d.pos:], "$") && !strings.HasPrefix(d.s[d.pos:], "$$") {
		return "", false, errors.New("unsupported template argument")
	}
	t, err := d.typ(true)
	if err != nil {
		return "", false, err
	}
	cv := (t.Kind == TypePrimitive || t.Kind == TypeNamed) && (t.Const || t.Volatile)
	return t.String(), cv, nil
}

// scope reads the enclosing scopes up to the terminating '@', outermost
// first in the result.
func (d *demangler) scope() ([]string, error) {
	var parts []string
	for {
		if d.peek() == 0 {
			return nil, errBad
		}
		if d.consume("@") {
			break
		}
		switch {
		case d.consume("?A"):
			// Anonymous namespace: ?A0x<hash>@
			if _, err := d.simpleName(); err != nil {
				return nil, err
			}
			parts = append(parts, "`anonymous namespace'")
			continue
		case strings.HasPrefix(d.s[d.pos:], "?") && !strings.HasPrefix(d.s[d.pos:], "?$"):
			// Function-local scope: ?<number>?<symbol>
			d.next()
			if d.peek() == '?' {
				d.next()
				sub := &demangler{s: d.s, pos: d.pos, depth: d.depth}
				sym, err := sub.symbol()
				if err != nil {
					return nil, err
				}
				d.pos = sub.pos
				parts = append(parts, "`"+sym.String()+"'")
				continue
			}
			n, err := d.number()
			if err != nil {
				return nil, err
			}
			parts = append(parts, fmt.Sprintf("`%d'", n))
			continue
		}
		n, _, err := d.unqualifiedName(false, true)
		if err != nil {
			return nil, err
		}
		parts = append(parts, n)
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return parts, nil
}

// number reads an encoded number: 0-9 for 1-10, else hex digits A-P ended
// by '@'; a leading '?' negates.
func (d *demangler) number() (int64, error) {
	neg := d.consume("?")
	c := d.peek()
	var v int64
	switch {
	case c >= '0' && c <= '9':
		d.next()
		v = int64(c-'0') + 1
	default:
		n := 0
		for d.peek() >= 'A' && d.peek() <= 'P' {
			v = v<<4 | int64(d.next()-'A')
			n++
		}
		if !d.consume("@") || n > 16 {
			return 0, errBad
		}
	}
	if neg {
		v = -v
	}
	return v, nil
}

var callConvs = map[byte]string{
	'A': "__cdecl", 'B': "__cdecl", 'C': "__pascal", 'D': "__pascal", 'E': "__thiscall", 'F': "__thiscall",
	'G': "__stdcall", 'H': "__stdcall", 'I': "__fastcall", 'J': "__fastcall", 'M': "__clrcall", 'N': "__clrcall",
	'O': "__eabi", 'P': "__eabi", 'Q': "__vectorcall", 'S': "__swift_1", 'W': "__swift_2",
}

// function reads a function's class/access code and signature.
func (d *demangler) function() (*Function, string, error) {
	f := &Function{}
	c := d.next()
	access := ""
	hasThis := false
	switch {
	case c == 'Y' || c == 'Z':
	case c >= 'A' && c <= 'X':
		f.Member = true
		idx := int(c - 'A')
		access = [...]string{"private", "protected", "public"}[idx/8]
		switch idx % 8 {
		case 0, 1:
			hasThis = true
		case 2, 3:
			f.Static = true
		case 4, 5:
			f.Virtual, hasThis = true, true
		case 6, 7:
			f.Virtual, hasThis, f.Thunk = true, true, true
			n, err := d.number()
			if err != nil {
				return nil, "", err
			}
			f.Adjustor = n
		}
	case c == '$':
		return nil, "", errors.New("unsupported vtordisp thunk")
	case c == '_':
		return nil, "", errors.New("unsupported function class")
	default:
		return nil, "", errBad
	}
	if hasThis {
		var q []string
		for {
			switch d.peek() {
			case 'E':
				d.next()
				q = append(q, "__ptr64")
				continue
			case 'I':
				d.next()
				q = append(q, "__restrict")
				continue
			case 'F':
				d.next()
				q = append(q, "__unaligned")
				continue
			}
			break
		}
		cv := d.cvName()
		if cv != "" {
			q = append([]string{cv}, q...)
		}
		f.ThisQuals = strings.Join(q, " ")
	}
	if err := d.signature(f); err != nil {
		return nil, "", err
	}
	return f, access, nil
}

// signature reads calling convention, return type and parameters.
func (d *demangler) signature(f *Function) error {
	cc, ok := callConvs[d.next()]
	if !ok {
		return errors.New("unknown calling convention")
	}
	f.CallConv = cc
	if !d.consume("@") {
		if d.consume("?") {
			// A return type with storage qualifiers (?A, ?B).
			q := d.cvName()
			t, err := d.typ(false)
			if err != nil {
				return err
			}
			applyQuals(t, q)
			f.Return = t
		} else {
			t, err := d.typ(false)
			if err != nil {
				return err
			}
			f.Return = t
		}
	}
	if d.consume("X") {
		// (void)
	} else {
		for {
			if d.consume("@") {
				break
			}
			if d.consume("Z") {
				f.Variadic = true
				break
			}
			if d.peek() == 0 {
				return errBad
			}
			start := d.pos
			c := d.peek()
			if c >= '0' && c <= '9' {
				d.next()
				i := int(c - '0')
				if i >= len(d.types) {
					return errors.New("type back-reference out of range")
				}
				f.Params = append(f.Params, d.types[i])
				continue
			}
			t, err := d.typ(false)
			if err != nil {
				return err
			}
			// Types longer than one character can be back-referenced.
			if d.pos-start > 1 && len(d.types) < 10 {
				d.types = append(d.types, t)
			}
			f.Params = append(f.Params, t)
		}
	}
	// Exception specification.
	switch {
	case d.consume("Z"):
	case d.consume("_E"):
	default:
		if d.peek() != 0 && d.peek() != '@' {
			return errors.New("unsupported exception specification")
		}
	}
	return nil
}

// cvName reads a cv letter: A none, B const, C volatile, D const volatile.
func (d *demangler) cvName() string {
	switch d.peek() {
	case 'A':
		d.next()
		return ""
	case 'B':
		d.next()
		return "const"
	case 'C':
		d.next()
		return "volatile"
	case 'D':
		d.next()
		return "const volatile"
	}
	return ""
}

// storageQuals reads a variable's trailing qualifiers: its own __ptr64 and
// cv qualifiers.
func (d *demangler) storageQuals() (ptr64 bool, cv string, err error) {
	for d.peek() == 'E' || d.peek() == 'F' || d.peek() == 'I' {
		if d.next() == 'E' {
			ptr64 = true
		}
	}
	if d.peek() == 0 {
		return false, "", errBad
	}
	return ptr64, d.cvName(), nil
}

func applyQuals(t *Type, q string) {
	if strings.Contains(q, "const") {
		t.Const = true
	}
	if strings.Contains(q, "volatile") {
		t.Volatile = true
	}
}

var primitives = map[byte]string{
	'X': "void", 'C': "signed char", 'D': "char", 'E': "unsigned char", 'F': "short", 'G': "unsigned short",
	'H': "int", 'I': "unsigned int", 'J': "long", 'K': "unsigned long", 'M': "float", 'N': "double",
	'O': "long double",
}

var extPrimitives = map[byte]string{
	'J': "__int64", 'K': "unsigned __int64", 'N': "bool", 'W': "wchar_t", 'S': "char16_t", 'U': "char32_t",
	'Q': "char8_t", 'L': "__int128", 'M': "unsigned __int128", 'D': "__int8", 'E': "unsigned __int8",
	'F': "__int16", 'G': "unsigned __int16", 'H': "__int32", 'I': "unsigned __int32",
}

// typ reads a type. inTemplate allows the extra forms of template
// arguments.
func (d *demangler) typ(inTemplate bool) (*Type, error) {
	d.depth++
	defer func() { d.depth-- }()
	if d.depth > 40 {
		return nil, errors.New("type nesting too deep")
	}
	c := d.peek()
	if p, ok := primitives[c]; ok {
		d.next()
		return &Type{Kind: TypePrimitive, Name: p}, nil
	}
	switch {
	case c == '_':
		d.next()
		p, ok := extPrimitives[d.next()]
		if !ok {
			return nil, errors.New("unknown extended type")
		}
		return &Type{Kind: TypePrimitive, Name: p}, nil
	case c == 'T' || c == 'U' || c == 'V':
		d.next()
		tag := map[byte]string{'T': "union", 'U': "struct", 'V': "class"}[c]
		n, err := d.qualifiedTypeName()
		if err != nil {
			return nil, err
		}
		return &Type{Kind: TypeNamed, Tag: tag, Name: n}, nil
	case c == 'W':
		d.next()
		u := map[byte]string{'0': "char", '1': "unsigned char", '2': "short", '3': "unsigned short",
			'4': "int", '5': "unsigned int", '6': "long", '7': "unsigned long"}[d.next()]
		n, err := d.qualifiedTypeName()
		if err != nil {
			return nil, err
		}
		return &Type{Kind: TypeNamed, Tag: "enum", Name: n, Underlying: u}, nil
	case c == 'P' || c == 'Q' || c == 'R' || c == 'S':
		d.next()
		t := &Type{Kind: TypePointer, PointerConst: c == 'Q' || c == 'S', PointerVolatile: c == 'R' || c == 'S'}
		return d.pointee(t)
	case c == 'A' || c == 'B':
		d.next()
		t := &Type{Kind: TypeReference, PointerVolatile: c == 'B'}
		return d.pointee(t)
	case d.consume("$$Q"), d.consume("$$R"):
		t := &Type{Kind: TypeRValueReference}
		return d.pointee(t)
	case d.consume("$$T"):
		return &Type{Kind: TypeNullptr, Name: "std::nullptr_t"}, nil
	case d.consume("$$C"):
		q := d.cvName()
		t, err := d.typ(inTemplate)
		if err != nil {
			return nil, err
		}
		applyQuals(t, q)
		return t, nil
	case d.consume("$$A6"), d.consume("$$A8@@"):
		f := &Function{}
		if err := d.signature(f); err != nil {
			return nil, err
		}
		return &Type{Kind: TypeFunction, Func: f}, nil
	case c == 'Y':
		d.next()
		n, err := d.number()
		if err != nil || n <= 0 || n > 8 {
			return nil, errBad
		}
		t := &Type{Kind: TypeArray}
		for i := int64(0); i < n; i++ {
			v, err := d.number()
			if err != nil {
				return nil, err
			}
			t.Dims = append(t.Dims, v)
		}
		el, err := d.typ(false)
		if err != nil {
			return nil, err
		}
		t.Pointee = el
		return t, nil
	case c == '?' && inTemplate:
		return nil, errors.New("unsupported template parameter")
	}
	return nil, fmt.Errorf("unsupported type code %q", c)
}

// pointee reads a pointer's or reference's qualifiers and target.
func (d *demangler) pointee(t *Type) (*Type, error) {
	for {
		switch d.peek() {
		case 'E':
			d.next()
			t.Ptr64 = true
			continue
		case 'I':
			d.next()
			t.Restrict = true
			continue
		case 'F':
			d.next()
			t.Unaligned = true
			continue
		}
		break
	}
	switch {
	case d.consume("6"):
		f := &Function{}
		if err := d.signature(f); err != nil {
			return nil, err
		}
		t.Pointee = &Type{Kind: TypeFunction, Func: f}
		return t, nil
	case d.consume("8"):
		cls, err := d.qualifiedTypeName()
		if err != nil {
			return nil, err
		}
		f := &Function{Member: true}
		var q []string
		for d.peek() == 'E' || d.peek() == 'I' || d.peek() == 'F' {
			if d.next() == 'E' {
				q = append(q, "__ptr64")
			}
		}
		if cv := d.cvName(); cv != "" {
			q = append([]string{cv}, q...)
		}
		f.ThisQuals = strings.Join(q, " ")
		if err := d.signature(f); err != nil {
			return nil, err
		}
		return &Type{Kind: TypeMemberPointer, Class: cls, Pointee: &Type{Kind: TypeFunction, Func: f}, Ptr64: t.Ptr64}, nil
	}
	switch c := d.peek(); c {
	case 'A', 'B', 'C', 'D':
		q := d.cvName()
		el, err := d.typ(false)
		if err != nil {
			return nil, err
		}
		applyQuals(el, q)
		t.Pointee = el
		return t, nil
	case 'Q', 'R', 'S', 'T':
		// Pointer to data member: cv letter shifted, then the class.
		d.next()
		cls, err := d.qualifiedTypeName()
		if err != nil {
			return nil, err
		}
		el, err := d.typ(false)
		if err != nil {
			return nil, err
		}
		applyQuals(el, map[byte]string{'Q': "", 'R': "const", 'S': "volatile", 'T': "const volatile"}[c])
		return &Type{Kind: TypeMemberPointer, Class: cls, Pointee: el, Ptr64: t.Ptr64}, nil
	}
	return nil, errors.New("unsupported pointer qualifier")
}

// qualifiedTypeName reads a named type's name: innermost first, ended by
// '@', joined outermost first.
func (d *demangler) qualifiedTypeName() (string, error) {
	first, _, err := d.unqualifiedName(false, true)
	if err != nil {
		return "", err
	}
	scope, err := d.scope()
	if err != nil {
		return "", err
	}
	return strings.Join(append(scope, first), "::"), nil
}

// QualifiedName joins Name with "::".
func (s *Symbol) QualifiedName() string { return strings.Join(s.Name, "::") }

// String renders the symbol as undname.exe does (default flags).
func (s *Symbol) String() string {
	var b strings.Builder
	switch s.Kind {
	case KindSpecial:
		if s.Quals != "" {
			b.WriteString(s.Quals + " ")
		}
		b.WriteString(s.QualifiedName())
		if s.SpecialFor != "" {
			b.WriteString("{for `" + s.SpecialFor + "'}")
		}
		return b.String()
	case KindData:
		if s.Access != "" {
			b.WriteString(s.Access + ": ")
		}
		if s.Storage != "" {
			b.WriteString(s.Storage + " ")
		}
		return b.String() + declare(s.Type, s.QualifiedName())
	case KindFunction:
		f := s.Func
		if f.Thunk {
			b.WriteString("[thunk]:")
		}
		if s.Access != "" {
			b.WriteString(s.Access + ": ")
		}
		if f.Static {
			b.WriteString("static ")
		}
		if f.Virtual {
			b.WriteString("virtual ")
		}
		name := s.QualifiedName()
		if f.Thunk {
			name += fmt.Sprintf("`adjustor{%d}' ", f.Adjustor)
		}
		head := name + "(" + paramList(f) + ")" + thisQuals(f.ThisQuals)
		if f.Return != nil && !f.Conversion {
			b.WriteString(declare(f.Return, f.CallConv+" "+head))
		} else {
			b.WriteString(f.CallConv + " " + head)
		}
		return b.String()
	}
	return s.QualifiedName()
}

// thisQuals renders a method's this qualifiers the way undname spaces
// them: cv against the parenthesis and always followed by a space, then
// the pointer qualifiers -- ")const __ptr64", ")const ", ") __ptr64".
func thisQuals(q string) string {
	if q == "" {
		return ""
	}
	var cv, rest []string
	for _, w := range strings.Fields(q) {
		if w == "const" || w == "volatile" {
			cv = append(cv, w)
		} else {
			rest = append(rest, w)
		}
	}
	return strings.Join(cv, " ") + " " + strings.Join(rest, " ")
}

func paramList(f *Function) string {
	var ps []string
	for _, p := range f.Params {
		ps = append(ps, p.String())
	}
	if f.Variadic {
		ps = append(ps, "...")
	}
	if len(ps) == 0 {
		return "void"
	}
	return strings.Join(ps, ",")
}

// String renders the type as undname does.
func (t *Type) String() string { return declare(t, "") }

// declare renders t declaring name (C declarator syntax, undname spacing).
func declare(t *Type, name string) string {
	join := func(a, b string) string {
		if b == "" {
			return a
		}
		if a == "" {
			return b
		}
		return a + " " + b
	}
	cv := func(t *Type, s string) string {
		if t.Const {
			s = join(s, "const")
		}
		if t.Volatile {
			s = join(s, "volatile")
		}
		return s
	}
	switch t.Kind {
	case TypePrimitive, TypeNullptr:
		return join(cv(t, t.Name), name)
	case TypeNamed:
		return join(cv(t, t.Tag+" "+t.Name), name)
	case TypePointer, TypeReference, TypeRValueReference:
		op := map[TypeKind]string{TypePointer: "*", TypeReference: "&", TypeRValueReference: "&&"}[t.Kind]
		if t.Ptr64 {
			op += " __ptr64"
		}
		if t.Restrict {
			op += " __restrict"
		}
		if t.PointerConst {
			op += " const"
		}
		if t.PointerVolatile && t.Kind == TypePointer {
			op += " volatile"
		}
		if t.storagePtr64 {
			op += " __ptr64"
		}
		inner := join(op, name)
		switch p := t.Pointee; {
		case p == nil:
			return inner
		case p.Kind == TypeFunction:
			// Inside a function-pointer declarator undname writes a bare "*"
			// against a following calling convention -- (__cdecl*__cdecl
			// f(void)) for a function returning one -- but not against a
			// variable name: (__cdecl* fp).
			if op == "*" && strings.HasPrefix(name, "__") {
				inner = op + name
			}
			return funcDecl(p.Func, "("+p.Func.CallConv+inner+")")
		case p.Kind == TypeArray:
			return declare(p, "("+inner+")")
		default:
			return declare(p, inner)
		}
	case TypeArray:
		dims := ""
		for _, n := range t.Dims {
			dims += fmt.Sprintf("[%d]", n)
		}
		return declare(t.Pointee, name+dims)
	case TypeFunction:
		return funcDecl(t.Func, join(t.Func.CallConv, name))
	case TypeMemberPointer:
		inner := t.Class + "::*"
		if t.Ptr64 {
			inner += " __ptr64"
		}
		inner = join(inner, name)
		if t.Pointee.Kind == TypeFunction {
			f := t.Pointee.Func
			return funcDecl(f, "("+f.CallConv+" "+inner+")") + thisQuals(f.ThisQuals)
		}
		return declare(t.Pointee, inner)
	}
	return name
}

// funcDecl renders a function type around decl, the already formed
// declarator ("(__cdecl*)", "__cdecl f").
func funcDecl(f *Function, decl string) string {
	head := decl + "(" + paramList(f) + ")"
	if f.Return == nil {
		return head
	}
	return declare(f.Return, head)
}
