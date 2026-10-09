// Package pdb reads Microsoft PDB (Program Database) 7.0 files: the MSF
// container, the PDB info stream, the DBI stream (modules, section headers),
// CodeView symbol records (procedures, locals, publics, globals) and the TPI
// and IPI type streams.
//
// The layout follows LLVM's "The PDB File Format" documentation and
// Microsoft's published microsoft-pdb sources (cvinfo.h). All integers are
// little-endian. The reader only reads; it never writes a PDB.
//
//	f, err := pdb.Open("app.pdb")
//	...
//	defer f.Close()
//	dbi, _ := f.DBI()
//	for _, m := range dbi.Modules {
//		syms, _ := f.ModuleSymbols(m)
//		...
//	}
package pdb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

var msfMagic = []byte("Microsoft C/C++ MSF 7.00\r\n\x1aDS\x00\x00\x00")

// MaxStreamSize bounds a single stream read. Type and symbol streams of very
// large programs are tens of megabytes; a corrupt size field must not make
// the reader allocate gigabytes.
const MaxStreamSize = 1 << 30

// Fixed stream indices.
const (
	StreamPDB = 1 // PDB info: version, signature, age, GUID
	StreamTPI = 2 // type records
	StreamDBI = 3 // debug info: modules, section map, stream indices
	StreamIPI = 4 // id records (LF_FUNC_ID, LF_STRING_ID, ...)
)

// ErrNotPDB is returned for input that is not an MSF 7.0 container.
var ErrNotPDB = errors.New("pdb: not a PDB 7.0 (MSF) file")

// File is an open PDB.
type File struct {
	r         io.ReaderAt
	closer    io.Closer
	blockSize uint32
	sizes     []uint32
	blocks    [][]uint32

	tpi, ipi *TypeTable // cached by Types and IDs
	dbi      *DBI       // cached by DBI
}

// Open opens the PDB at path.
func Open(path string) (*File, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	f, err := NewFile(fh)
	if err != nil {
		fh.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	f.closer = fh
	return f, nil
}

// NewFile reads a PDB from r. The caller keeps ownership of r.
func NewFile(r io.ReaderAt) (*File, error) {
	hdr := make([]byte, 56)
	if _, err := r.ReadAt(hdr, 0); err != nil || !bytes.Equal(hdr[:32], msfMagic) {
		return nil, ErrNotPDB
	}
	le := binary.LittleEndian
	f := &File{r: r, blockSize: le.Uint32(hdr[32:])}
	numBlocks := le.Uint32(hdr[40:])
	dirBytes := le.Uint32(hdr[44:])
	blockMapAddr := le.Uint32(hdr[52:])
	switch f.blockSize {
	case 512, 1024, 2048, 4096, 8192, 16384, 32768:
	default:
		return nil, fmt.Errorf("pdb: unsupported MSF block size %d", f.blockSize)
	}
	if dirBytes == 0 || dirBytes > MaxStreamSize || blockMapAddr >= numBlocks {
		return nil, errors.New("pdb: corrupt MSF directory")
	}
	// The block map lists the blocks holding the stream directory.
	nDirBlocks := (dirBytes + f.blockSize - 1) / f.blockSize
	mapBuf := make([]byte, nDirBlocks*4)
	if _, err := r.ReadAt(mapBuf, int64(blockMapAddr)*int64(f.blockSize)); err != nil {
		return nil, fmt.Errorf("pdb: MSF block map: %w", err)
	}
	dirBlocks := make([]uint32, nDirBlocks)
	for i := range dirBlocks {
		dirBlocks[i] = le.Uint32(mapBuf[i*4:])
	}
	dir, err := f.readBlocks(dirBlocks, dirBytes)
	if err != nil {
		return nil, fmt.Errorf("pdb: MSF directory: %w", err)
	}
	if len(dir) < 4 {
		return nil, errors.New("pdb: corrupt MSF directory")
	}
	n := le.Uint32(dir)
	if uint64(n)*4+4 > uint64(len(dir)) {
		return nil, errors.New("pdb: corrupt MSF stream count")
	}
	f.sizes = make([]uint32, n)
	for i := range f.sizes {
		f.sizes[i] = le.Uint32(dir[4+i*4:])
	}
	pos := 4 + int(n)*4
	f.blocks = make([][]uint32, n)
	for i, size := range f.sizes {
		if size == 0xffffffff { // deleted stream
			f.sizes[i] = 0
			continue
		}
		cnt := int((size + f.blockSize - 1) / f.blockSize)
		if pos+cnt*4 > len(dir) {
			return nil, errors.New("pdb: corrupt MSF stream block list")
		}
		bl := make([]uint32, cnt)
		for j := range bl {
			bl[j] = le.Uint32(dir[pos+j*4:])
		}
		f.blocks[i] = bl
		pos += cnt * 4
	}
	return f, nil
}

// Close closes a File opened with Open.
func (f *File) Close() error {
	if f.closer != nil {
		return f.closer.Close()
	}
	return nil
}

// NumStreams is the number of streams in the directory.
func (f *File) NumStreams() int { return len(f.sizes) }

// StreamSize is the byte size of stream i (0 for a missing stream).
func (f *File) StreamSize(i int) uint32 {
	if i < 0 || i >= len(f.sizes) {
		return 0
	}
	return f.sizes[i]
}

// Stream returns the content of stream i, nil for an empty or missing one.
func (f *File) Stream(i int) ([]byte, error) {
	if i < 0 || i >= len(f.sizes) || f.sizes[i] == 0 {
		return nil, nil
	}
	return f.readBlocks(f.blocks[i], f.sizes[i])
}

func (f *File) readBlocks(blocks []uint32, size uint32) ([]byte, error) {
	if size > MaxStreamSize {
		return nil, fmt.Errorf("pdb: stream of %d bytes exceeds the %d-byte limit", size, MaxStreamSize)
	}
	out := make([]byte, size)
	// Streams are mostly laid out in consecutive blocks: one read per run
	// instead of per block (a large PDB has hundreds of thousands).
	for i := 0; i < len(blocks); {
		lo := uint32(i) * f.blockSize
		if lo >= size {
			break
		}
		j := i + 1
		for j < len(blocks) && blocks[j] == blocks[j-1]+1 && uint32(j)*f.blockSize < size {
			j++
		}
		hi := min(uint32(j)*f.blockSize, size)
		if _, err := f.r.ReadAt(out[lo:hi], int64(blocks[i])*int64(f.blockSize)); err != nil && err != io.EOF {
			return nil, err
		}
		i = j
	}
	return out, nil
}

// Info is the PDB info stream: the identity a PE image's RSDS debug record
// names.
type Info struct {
	Version   uint32
	Signature uint32
	Age       uint32
	GUID      GUID
}

// GUID is a PDB/RSDS GUID in its on-disk byte order.
type GUID [16]byte

// String formats the GUID the way symbol servers and debuggers print it.
func (g GUID) String() string {
	le := binary.LittleEndian
	return fmt.Sprintf("%08X-%04X-%04X-%X-%X", le.Uint32(g[0:]), le.Uint16(g[4:]), le.Uint16(g[6:]), g[8:10], g[10:])
}

// Info reads the PDB info stream.
func (f *File) Info() (*Info, error) {
	s, err := f.Stream(StreamPDB)
	if err != nil {
		return nil, err
	}
	if len(s) < 28 {
		return nil, errors.New("pdb: info stream too short")
	}
	le := binary.LittleEndian
	in := &Info{Version: le.Uint32(s), Signature: le.Uint32(s[4:]), Age: le.Uint32(s[8:])}
	copy(in.GUID[:], s[12:28])
	return in, nil
}

// cString returns b up to its first NUL.
func cString(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}
