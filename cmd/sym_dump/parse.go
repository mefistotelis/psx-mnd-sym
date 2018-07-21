package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/sanctuary/sym"
	"github.com/sanctuary/sym/internal/c"
)

// Prefix added to duplicate symbols.
const duplicatePrefix = "_duplicate_"

// parseDecls parses the SYM symbols into the equivalent C declarations.
func (p *parser) parseDecls(syms []*sym.Symbol) {
	var curLine Line
	for i := 0; i < len(syms); i++ {
		s := syms[i]
		switch body := s.Body.(type) {
		case *sym.Name1:
			symbol := &Symbol{
				Addr: s.Hdr.Value,
				Name: body.Name,
			}
			p.curOverlay.symbols = append(p.curOverlay.symbols, symbol)
		case *sym.Name2:
			symbol := &Symbol{
				Addr: s.Hdr.Value,
				Name: body.Name,
			}
			p.curOverlay.symbols = append(p.curOverlay.symbols, symbol)
		case *sym.IncSLD:
			curLine.Line++
			line := &Line{
				Addr: s.Hdr.Value,
				Path: curLine.Path,
				Line: curLine.Line,
			}
			p.curOverlay.lines = append(p.curOverlay.lines, line)
		case *sym.IncSLDByte:
			curLine.Line += uint32(body.Inc)
			line := &Line{
				Addr: s.Hdr.Value,
				Path: curLine.Path,
				Line: curLine.Line,
			}
			p.curOverlay.lines = append(p.curOverlay.lines, line)
		case *sym.IncSLDWord:
			curLine.Line += uint32(body.Inc)
			line := &Line{
				Addr: s.Hdr.Value,
				Path: curLine.Path,
				Line: curLine.Line,
			}
			p.curOverlay.lines = append(p.curOverlay.lines, line)
		case *sym.SetSLD:
			// TODO: reset curLine.Path?
			curLine.Line = body.Line
			line := &Line{
				Addr: s.Hdr.Value,
				Path: curLine.Path,
				Line: curLine.Line,
			}
			p.curOverlay.lines = append(p.curOverlay.lines, line)
		case *sym.SetSLD2:
			curLine = Line{
				Path: body.Path,
				Line: body.Line,
			}
			line := &Line{
				Addr: s.Hdr.Value,
				Path: curLine.Path,
				Line: curLine.Line,
			}
			p.curOverlay.lines = append(p.curOverlay.lines, line)
		case *sym.EndSLD:
			curLine = Line{}
		case *sym.FuncStart:
			n := p.parseFunc(s.Hdr.Value, body, syms[i+1:])
			i += n
		case *sym.Def:
			switch body.Class {
			case sym.ClassEXT, sym.ClassSTAT:
				p.parseDef(s.Hdr.Value, body.Size, body.Class, body.Type, nil, "", body.Name)
			case sym.ClassMOS, sym.ClassSTRTAG, sym.ClassMOU, sym.ClassUNTAG, sym.ClassTPDEF, sym.ClassENTAG, sym.ClassMOE, sym.ClassFIELD:
				// nothing to do.
			default:
				panic(fmt.Sprintf("support for symbol class %q not yet implemented", body.Class))
			}
		case *sym.Def2:
			switch body.Class {
			case sym.ClassEXT, sym.ClassSTAT:
				p.parseDef(s.Hdr.Value, body.Size, body.Class, body.Type, body.Dims, body.Tag, body.Name)
			case sym.ClassMOS, sym.ClassMOU, sym.ClassTPDEF, sym.ClassMOE, sym.ClassFIELD, sym.ClassEOS:
				// nothing to do.
			default:
				panic(fmt.Sprintf("support for symbol class %q not yet implemented", body.Class))
			}
		case *sym.Overlay:
			p.parseOverlay(s.Hdr.Value, body)
		case *sym.SetOverlay:
			overlay, ok := p.overlayIDs[s.Hdr.Value]
			if !ok {
				panic(fmt.Errorf("unable to locate overlay with ID %x", s.Hdr.Value))
			}
			p.curOverlay = overlay
		default:
			panic(fmt.Sprintf("support for symbol type %T not yet implemented", body))
		}
	}
}

// parseFunc parses a function sequence of symbols.
func (p *parser) parseFunc(addr uint32, body *sym.FuncStart, syms []*sym.Symbol) (n int) {
	name := validName(body.Name)
	f, ok := p.curOverlay.funcNames[name]
	if !ok {
		panic(fmt.Errorf("unable to locate function %q", name))
	}
	if f.Addr != addr {
		name = uniqueName(name, addr)
		f, ok = p.curOverlay.funcNames[name]
		if !ok {
			panic(fmt.Errorf("unable to locate function %q", name))
		}
	}
	funcType, ok := f.Type.(*c.FuncType)
	if !ok {
		panic(fmt.Errorf("invalid function type; expected *c.FuncType, got %T", f.Type))
	}
	if f.LineStart != 0 {
		// Ignore function duplicate.
		for n = 0; n < len(syms); n++ {
			s := syms[n]
			switch s.Body.(type) {
			case *sym.FuncEnd:
				return n + 1
			}
		}
	}
	f.LineStart = body.Line
	var blocks blockStack
	var curBlock *c.Block
	for n = 0; n < len(syms); n++ {
		s := syms[n]
		switch body := s.Body.(type) {
		case *sym.FuncEnd:
			f.LineEnd = body.Line
			return n + 1
		case *sym.BlockStart:
			if curBlock != nil {
				blocks.push(curBlock)
			}
			block := &c.Block{
				LineStart: body.Line,
			}
			f.Blocks = append(f.Blocks, block)
			curBlock = block
		case *sym.BlockEnd:
			curBlock.LineEnd = body.Line
			if !blocks.empty() {
				curBlock = blocks.pop()
			}
		case *sym.Def:
			switch body.Class {
			case sym.ClassAUTO, sym.ClassSTAT, sym.ClassREG, sym.ClassLABEL, sym.ClassARG, sym.ClassREGPARM:
				v := c.Var{
					Type: p.parseType(body.Type, nil, ""),
					Name: body.Name,
				}
				if curBlock != nil {
					addLocal(curBlock, v)
				} else {
					addParam(funcType, v)
				}
			default:
				panic(fmt.Errorf("support for symbol class %q not yet implemented", body.Class))
			}
		case *sym.Def2:
			switch body.Class {
			case sym.ClassAUTO, sym.ClassSTAT, sym.ClassREG, sym.ClassLABEL, sym.ClassARG, sym.ClassREGPARM:
				v := c.Var{
					Type: p.parseType(body.Type, body.Dims, body.Tag),
					Name: body.Name,
				}
				if curBlock != nil {
					addLocal(curBlock, v)
				} else {
					addParam(funcType, v)
				}
			default:
				panic(fmt.Errorf("support for symbol class %q not yet implemented", body.Class))
			}
		default:
			panic(fmt.Errorf("support for symbol type %T not yet implemented", body))
		}
	}
	panic("unreachable")
}

// parseOverlay parses an overlay symbol.
func (p *parser) parseOverlay(addr uint32, body *sym.Overlay) {
	overlay := &Overlay{
		Addr:      addr,
		ID:        body.ID,
		Length:    body.Length,
		varNames:  make(map[string]*c.VarDecl),
		funcNames: make(map[string]*c.FuncDecl),
	}
	p.overlays = append(p.overlays, overlay)
	p.overlayIDs[overlay.ID] = overlay
}

// parseDef parses a definition symbol.
func (p *parser) parseDef(addr, size uint32, class sym.Class, t sym.Type, dims []uint32, tag, name string) {
	name = validName(name)
	// Duplicate name.
	if _, ok := p.curOverlay.varNames[name]; ok {
		name = uniqueName(name, addr)
	} else if _, ok := p.curOverlay.funcNames[name]; ok {
		name = uniqueName(name, addr)
	}
	storageClass := parseClass(class)
	cType := p.parseType(t, dims, tag)
	if funcType, ok := cType.(*c.FuncType); ok {
		f := &c.FuncDecl{
			Addr: addr,
			Size: size,
			Var: c.Var{
				Type: funcType,
				Name: name,
			},
		}
		p.curOverlay.funcs = append(p.curOverlay.funcs, f)
		p.curOverlay.funcNames[name] = f
	} else {
		v := &c.VarDecl{
			Addr:         addr,
			Size:         size,
			StorageClass: storageClass,
			Var: c.Var{
				Type: cType,
				Name: name,
			},
		}
		p.curOverlay.vars = append(p.curOverlay.vars, v)
		p.curOverlay.varNames[name] = v
	}
}

// parseTypes parses the SYM types into the equivalent C types.
func (p *parser) parseTypes(syms []*sym.Symbol) {
	p.initTaggedTypes(syms)
	// Parse symbols.
	for i := 0; i < len(syms); i++ {
		s := syms[i]
		// TODO: remove debug output once C output is mature.
		//fmt.Fprintln(os.Stderr, "sym:", s)
		switch body := s.Body.(type) {
		case *sym.Def:
			switch body.Class {
			case sym.ClassSTRTAG:
				n := p.parseClassSTRTAG(body, syms[i+1:])
				i += n
			case sym.ClassUNTAG:
				n := p.parseClassUNTAG(body, syms[i+1:])
				i += n
			case sym.ClassTPDEF:
				p.parseClassTPDEF(body.Type, nil, "", body.Name)
			case sym.ClassENTAG:
				n := p.parseClassENTAG(body, syms[i+1:])
				i += n
			default:
				//log.Printf("support for class %q not yet implemented", body.Class)
			}
		case *sym.Def2:
			switch body.Class {
			case sym.ClassTPDEF:
				p.parseClassTPDEF(body.Type, body.Dims, body.Tag, body.Name)
			default:
				//log.Printf("support for class %q not yet implemented", body.Class)
			}
		case *sym.Overlay:
		// nothing to do.
		default:
			//log.Printf("support for symbol body %T not yet implemented", body)
		}
	}
}

// initTaggedTypes adds scaffolding types for structs, unions and enums.
func (p *parser) initTaggedTypes(syms []*sym.Symbol) {
	// Add scaffolding types for structs, unions and enums, so they may be
	// referrenced before defined.
	vtblPtrType := &c.StructType{
		Tag: "__vtbl_ptr_type",
	}
	p.structs["__vtbl_ptr_type"] = vtblPtrType
	p.structTags = append(p.structTags, "__vtbl_ptr_type")
	// Bool used for NULL type.
	boolDef := &c.VarDecl{
		StorageClass: c.Typedef,
		Var: c.Var{
			Type: c.Int,
			Name: "bool",
		},
	}
	p.types["bool"] = boolDef
	var (
		uniqueStruct = make(map[string]bool)
		uniqueUnion  = make(map[string]bool)
		uniqueEnum   = make(map[string]bool)
	)
	for _, s := range syms {
		switch body := s.Body.(type) {
		case *sym.Def:
			switch body.Class {
			case sym.ClassSTRTAG:
				tag := validName(body.Name)
				if uniqueStruct[tag] {
					tag = duplicatePrefix + tag
				}
				uniqueStruct[tag] = true
				t := &c.StructType{
					Size: body.Size,
					Tag:  tag,
				}
				p.structs[tag] = t
				p.structTags = append(p.structTags, tag)
			case sym.ClassUNTAG:
				tag := validName(body.Name)
				if uniqueUnion[tag] {
					tag = duplicatePrefix + tag
				}
				uniqueUnion[tag] = true
				t := &c.UnionType{
					Size: body.Size,
					Tag:  tag,
				}
				p.unions[tag] = t
				p.unionTags = append(p.unionTags, tag)
			case sym.ClassENTAG:
				tag := validName(body.Name)
				if uniqueEnum[tag] {
					tag = duplicatePrefix + tag
				}
				uniqueEnum[tag] = true
				t := &c.EnumType{
					Tag: tag,
				}
				p.enums[tag] = t
				p.enumTags = append(p.enumTags, tag)
			}
		}
	}
}

// parser tracks type information used for parsing.
type parser struct {
	// Type information.

	// structs maps from struct tag to struct type.
	structs map[string]*c.StructType
	// unions maps from union tag to union type.
	unions map[string]*c.UnionType
	// enums maps from enum tag to enum type.
	enums map[string]*c.EnumType
	// types maps from type name to underlying type definition.
	types map[string]c.Type
	// Struct tags in order of occurrence in SYM file.
	structTags []string
	// Union tags in order of occurrence in SYM file.
	unionTags []string
	// Enum tags in order of occurrence in SYM file.
	enumTags []string
	// Type definitions in order of occurrence in SYM file.
	typedefs []c.Type
	// Tracks unique enum member names.
	uniqueEnumMember map[string]bool

	// Declarations.
	*Overlay // default binary

	// Overlays.
	overlays []*Overlay
	// overlayIDs maps from overlay ID to overlay.
	overlayIDs map[uint32]*Overlay

	// Current overlay.
	curOverlay *Overlay
}

// An Overlay is an overlay appended to the end of the executable.
type Overlay struct {
	// Base address at which the overlay is loaded.
	Addr uint32
	// Overlay ID.
	ID uint32
	// Overlay length in bytes.
	Length uint32

	// Variable delcarations.
	vars []*c.VarDecl
	// Function delcarations.
	funcs []*c.FuncDecl
	// varNames maps from variable name to variable declaration.
	varNames map[string]*c.VarDecl
	// funcNames maps from function name to function declaration.
	funcNames map[string]*c.FuncDecl

	// Symbols.
	symbols []*Symbol
	// Source file line numbers.
	lines []*Line
}

// A Symbol associates a symbol name with an address.
type Symbol struct {
	// Symbol address.
	Addr uint32
	// Symbol name.
	Name string
}

// A Line associates a line number in a source file with an address.
type Line struct {
	// Address.
	Addr uint32
	// Source file name.
	Path string
	// Line number.
	Line uint32
}

// newParser returns a new parser.
func newParser() *parser {
	overlay := &Overlay{
		varNames:  make(map[string]*c.VarDecl),
		funcNames: make(map[string]*c.FuncDecl),
	}
	return &parser{
		structs:          make(map[string]*c.StructType),
		unions:           make(map[string]*c.UnionType),
		enums:            make(map[string]*c.EnumType),
		types:            make(map[string]c.Type),
		uniqueEnumMember: make(map[string]bool),
		Overlay:          overlay,
		overlayIDs:       make(map[uint32]*Overlay),
		curOverlay:       overlay,
	}
}

// parseClassSTRTAG parses a struct tag sequence of symbols.
func (p *parser) parseClassSTRTAG(body *sym.Def, syms []*sym.Symbol) (n int) {
	if base := body.Type.Base(); base != sym.BaseStruct {
		panic(fmt.Errorf("support for base type %q not yet implemented", base))
	}
	name := validName(body.Name)
	t, ok := p.structs[name]
	if !ok {
		panic(fmt.Errorf("unable to locate struct %q", name))
	}
	if len(t.Fields) > 0 {
		log.Printf("duplicate struct tag %q symbol", name)
		dupTag := duplicatePrefix + name
		t = &c.StructType{
			Size: body.Size,
			Tag:  dupTag,
		}
		p.structs[dupTag] = t
	}
	for n = 0; n < len(syms); n++ {
		s := syms[n]
		switch body := s.Body.(type) {
		case *sym.Def:
			switch body.Class {
			case sym.ClassMOS, sym.ClassFIELD:
				// TODO: Figure out how to handle FIELD. For now, parse as MOS.
				field := c.Field{
					Offset: s.Hdr.Value,
					Size:   body.Size,
					Var: c.Var{
						Type: p.parseType(body.Type, nil, ""),
						Name: validName(body.Name),
					},
				}
				t.Fields = append(t.Fields, field)
			default:
				panic(fmt.Errorf("support for class %q not yet implemented", body.Class))
			}
		case *sym.Def2:
			switch body.Class {
			case sym.ClassMOS:
				field := c.Field{
					Offset: s.Hdr.Value,
					Size:   body.Size,
					Var: c.Var{
						Type: p.parseType(body.Type, body.Dims, body.Tag),
						Name: validName(body.Name),
					},
				}
				t.Fields = append(t.Fields, field)
			case sym.ClassEOS:
				return n + 1
			default:
				panic(fmt.Errorf("support for class %q not yet implemented", body.Class))
			}
		}
	}
	panic("unreachable")
}

// parseClassUNTAG parses a union tag sequence of symbols.
func (p *parser) parseClassUNTAG(body *sym.Def, syms []*sym.Symbol) (n int) {
	if base := body.Type.Base(); base != sym.BaseUnion {
		panic(fmt.Errorf("support for base type %q not yet implemented", base))
	}
	name := validName(body.Name)
	t, ok := p.unions[name]
	if !ok {
		panic(fmt.Errorf("unable to locate union %q", name))
	}
	for n = 0; n < len(syms); n++ {
		s := syms[n]
		switch body := s.Body.(type) {
		case *sym.Def:
			switch body.Class {
			case sym.ClassMOU:
				field := c.Field{
					Offset: s.Hdr.Value,
					Size:   body.Size,
					Var: c.Var{
						Type: p.parseType(body.Type, nil, ""),
						Name: validName(body.Name),
					},
				}
				t.Fields = append(t.Fields, field)
			default:
				panic(fmt.Errorf("support for class %q not yet implemented", body.Class))
			}
		case *sym.Def2:
			switch body.Class {
			case sym.ClassMOU:
				field := c.Field{
					Offset: s.Hdr.Value,
					Size:   body.Size,
					Var: c.Var{
						Type: p.parseType(body.Type, body.Dims, body.Tag),
						Name: validName(body.Name),
					},
				}
				t.Fields = append(t.Fields, field)
			case sym.ClassEOS:
				return n + 1
			default:
				panic(fmt.Errorf("support for class %q not yet implemented", body.Class))
			}
		}
	}
	panic("unreachable")
}

// parseClassTPDEF parses a typedef symbol.
func (p *parser) parseClassTPDEF(t sym.Type, dims []uint32, tag, name string) {
	name = validName(name)
	def := &c.VarDecl{
		StorageClass: c.Typedef,
		Var: c.Var{
			Type: p.parseType(t, dims, tag),
			Name: name,
		},
	}
	p.typedefs = append(p.typedefs, def)
	p.types[name] = def
}

// parseClassENTAG parses an enum tag sequence of symbols.
func (p *parser) parseClassENTAG(body *sym.Def, syms []*sym.Symbol) (n int) {
	if base := body.Type.Base(); base != sym.BaseEnum {
		panic(fmt.Errorf("support for base type %q not yet implemented", base))
	}
	name := validName(body.Name)
	t, ok := p.enums[name]
	if !ok {
		panic(fmt.Errorf("unable to locate enum %q", name))
	}
	for n = 0; n < len(syms); n++ {
		s := syms[n]
		switch body := s.Body.(type) {
		case *sym.Def:
			switch body.Class {
			case sym.ClassMOE:
				name := validName(body.Name)
				if p.uniqueEnumMember[name] {
					name = strings.ToUpper(duplicatePrefix) + name
				}
				p.uniqueEnumMember[name] = true
				member := &c.EnumMember{
					Value: s.Hdr.Value,
					Name:  name,
				}
				t.Members = append(t.Members, member)
			default:
				panic(fmt.Errorf("support for class %q not yet implemented", body.Class))
			}
		case *sym.Def2:
			switch body.Class {
			case sym.ClassEOS:
				return n + 1
			default:
				panic(fmt.Errorf("support for class %q not yet implemented", body.Class))
			}
		}
	}
	panic("unreachable")
}

// ### [ Helper functions ] ####################################################

// parseType parses the SYM type into the equivalent C type.
func (p *parser) parseType(t sym.Type, dims []uint32, tag string) c.Type {
	u := p.parseBase(t.Base(), tag)
	return parseMods(u, t.Mods(), dims)
}

// parseBase parses the SYM base type into the equivalent C type.
func (p *parser) parseBase(base sym.Base, tag string) c.Type {
	tag = validName(tag)
	switch base {
	case sym.BaseNull:
		return p.types["bool"]
	case sym.BaseVoid:
		return c.Void
	case sym.BaseChar:
		return c.Char
	case sym.BaseShort:
		return c.Short
	case sym.BaseInt:
		return c.Int
	case sym.BaseLong:
		return c.Long
	case sym.BaseStruct:
		t, ok := p.structs[tag]
		if !ok {
			panic(fmt.Errorf("unable to locate struct %q", tag))
		}
		return t
	case sym.BaseUnion:
		t, ok := p.unions[tag]
		if !ok {
			panic(fmt.Errorf("unable to locate union %q", tag))
		}
		return t
	case sym.BaseEnum:
		t, ok := p.enums[tag]
		if !ok {
			panic(fmt.Errorf("unable to locate enum %q", tag))
		}
		return t
	//case sym.BaseMOE:
	case sym.BaseUChar:
		return c.UChar
	case sym.BaseUShort:
		return c.UShort
	case sym.BaseUInt:
		return c.UInt
	case sym.BaseULong:
		return c.ULong
	default:
		panic(fmt.Errorf("base type %q not yet supported", base))
	}
}

// parseMods parses the SYM type modifiers into the equivalent C type modifiers.
func parseMods(t c.Type, mods []sym.Mod, dims []uint32) c.Type {
	j := 0
	// TODO: consider rewriting c.Type.Mods to calculate mask from right to left
	// instead of left to right.
	for i := len(mods) - 1; i >= 0; i-- {
		mod := mods[i]
		switch mod {
		case sym.ModPointer:
			t = &c.PointerType{Elem: t}
		case sym.ModFunction:
			// TODO: Figure out how to store params and variadic.
			t = &c.FuncType{
				RetType: t,
			}
		case sym.ModArray:
			t = &c.ArrayType{
				Elem: t,
				Len:  int(dims[j]),
			}
			j++
		}
	}
	return t
}

// validName returns a valid C identifier based on the given name.
func validName(name string) string {
	f := func(r rune) rune {
		switch {
		case 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9':
			return r
		default:
			return '_'
		}
	}
	return strings.Map(f, name)
}

// blockStack is a stack of blocks.
type blockStack []*c.Block

// push pushes the block onto the stack.
func (b *blockStack) push(block *c.Block) {
	*b = append(*b, block)
}

// pop pops the block from the stack.
func (b *blockStack) pop() *c.Block {
	if b.empty() {
		panic("invalid pop call; empty stack")
	}
	block := (*b)[len(*b)-1]
	*b = (*b)[:len(*b)-1]
	return block
}

// empty reports if the stack is empty.
func (b *blockStack) empty() bool {
	return len(*b) == 0
}

// addLocal adds the local variable to the block if not already present.
func addLocal(block *c.Block, local c.Var) {
	for _, v := range block.Locals {
		if v.Name == local.Name {
			return
		}
	}
	block.Locals = append(block.Locals, local)
}

// addParam adds the function parameter to the function type if not already
// present.
func addParam(t *c.FuncType, param c.Var) {
	for _, p := range t.Params {
		if p.Name == param.Name {
			return
		}
	}
	t.Params = append(t.Params, param)
}

// uniqueName returns a unique name based on the given name and address.
func uniqueName(name string, addr uint32) string {
	return fmt.Sprintf("%s_addr_%08X", name, addr)
}

// parseClass parses the symbol class into an equivalent C storage class.
func parseClass(class sym.Class) c.StorageClass {
	switch class {
	case sym.ClassAUTO:
		return c.Auto
	case sym.ClassEXT:
		return c.Extern
	case sym.ClassSTAT:
		return c.Static
	case sym.ClassREG:
		return c.Register
	case sym.ClassTPDEF:
		return c.Typedef
	default:
		panic(fmt.Errorf("support for symbol class %v not yet implemented", class))
	}
}
