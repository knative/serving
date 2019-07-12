// Package tsz implement time-series compression
/*

http://www.vldb.org/pvldb/vol8/p1816-teller.pdf

*/
package tsz

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"math/bits"
	"sync"
)

// Series is the basic series primitive
// you can concurrently put values, finish the stream, and create iterators
type Series struct {
	sync.Mutex

	T0  uint64
	t   uint64
	val float64

	bw       bstream
	leading  uint8
	trailing uint8
	finished bool

	tDelta uint32
}

// New series
func New(t0 uint64) *Series {
	s := Series{
		T0:      t0,
		leading: ^uint8(0),
	}

	// block header
	s.bw.writeBits(uint64(t0), 64)

	return &s

}

// Bytes value of the series stream
func (s *Series) Bytes() []byte {
	s.Lock()
	defer s.Unlock()
	return s.bw.bytes()
}

func finish(w *bstream) {
	// write an end-of-stream record
	w.writeBits(0x0f, 4)
	w.writeBits(0xffffffff, 32)
	w.writeBit(zero)
}

// Finish the series by writing an end-of-stream record
func (s *Series) Finish() {
	s.Lock()
	if !s.finished {
		finish(&s.bw)
		s.finished = true
	}
	s.Unlock()
}

// Push a timestamp and value to the series.
// Values must be inserted in monotonically increasing time order.
func (s *Series) Push(t uint64, v float64) {
	s.Lock()
	defer s.Unlock()

	if s.t == 0 {
		// first point
		s.t = t
		s.val = v
		s.tDelta = uint32(t - s.T0)
		s.bw.writeBits(uint64(s.tDelta), 27)
		s.bw.writeBits(math.Float64bits(v), 64)
		return
	}

	// Difference to the original Facebook paper, we store the first delta as 27
	// bits to allow millisecond accuracy for a one day block.
	tDelta := uint32(t - s.t)
	d := int32(tDelta - s.tDelta)

	if d == 0 {
		s.bw.writeBit(zero)
	} else {
		// Increase by one in the decompressing phase as we have one free bit
		switch dod := encodeZigZag32(d) - 1; 32 - bits.LeadingZeros32(dod) {
		case 1, 2, 3, 4, 5, 6, 7:
			s.bw.writeBits(uint64(dod|256), 9) // dod | 00000000000000000000000100000000
		case 8, 9:
			s.bw.writeBits(uint64(dod|3072), 12) // dod | 00000000000000000000110000000000
		case 10, 11, 12:
			s.bw.writeBits(uint64(dod|57344), 16) // dod | 00000000000000001110000000000000
		default:
			s.bw.writeBits(0x0f, 4) // '1111'
			s.bw.writeBits(uint64(dod), 32)
		}
	}

	vDelta := math.Float64bits(s.val) ^ math.Float64bits(v)

	if vDelta == 0 {
		s.bw.writeBit(zero)
	} else {
		leading := uint8(bits.LeadingZeros64(vDelta))
		trailing := uint8(bits.TrailingZeros64(vDelta))

		s.bw.writeBit(one)

		if leading >= s.leading && trailing >= s.trailing {
			s.bw.writeBit(zero)
			s.bw.writeBits(vDelta>>s.trailing, 64-int(s.leading)-int(s.trailing))
		} else {
			s.bw.writeBit(one)

			// Different from version 1.x, use (significantBits - 1) in storage - avoids a branch
			sigbits := 64 - leading - trailing

			// Different from original, bits 5 -> 6, avoids a branch, allows storing small longs
			s.bw.writeBits(uint64(leading), 6)             // Number of leading zeros in the next 6 bits
			s.bw.writeBits(uint64(sigbits-1), 6)           // Length of meaningful bits in the next 6 bits
			s.bw.writeBits(vDelta>>trailing, int(sigbits)) // Store the meaningful bits of XOR

			s.leading, s.trailing = leading, trailing
		}
	}

	s.tDelta = tDelta
	s.t = t
	s.val = v

}

// Iter lets you iterate over a series.  It is not concurrency-safe.
func (s *Series) Iter() *Iter {
	s.Lock()
	w := s.bw.clone()
	s.Unlock()

	finish(w)
	iter, _ := bstreamIterator(w)
	return iter
}

// Iter lets you iterate over a series.  It is not concurrency-safe.
type Iter struct {
	T0 uint64

	t   uint64
	val float64

	br       bstream
	leading  uint8
	trailing uint8

	finished bool

	tDelta uint32
	err    error
}

func bstreamIterator(br *bstream) (*Iter, error) {

	br.count = 8

	t0, err := br.readBits(64)
	if err != nil {
		return nil, err
	}

	return &Iter{
		T0: uint64(t0),
		br: *br,
	}, nil
}

// NewIterator for the series
func NewIterator(b []byte) (*Iter, error) {
	return bstreamIterator(newBReader(b))
}

// Next iteration of the series iterator
func (it *Iter) Next() bool {

	if it.err != nil || it.finished {
		return false
	}

	if it.t == 0 {
		// read first t and v
		tDelta, err := it.br.readBits(27)
		if err != nil {
			it.err = err
			return false
		}

		if tDelta == (1<<27)-1 {
			it.finished = true
			return false
		}

		it.tDelta = uint32(tDelta)
		it.t = it.T0 + tDelta
		v, err := it.br.readBits(64)
		if err != nil {
			it.err = err
			return false
		}

		it.val = math.Float64frombits(v)

		return true
	}

	// read delta-of-delta
	d, err := it.br.readUntilZero(4)
	if err != nil {
		it.err = err
		return false
	}

	if d != 0 {
		var sz uint
		switch d {
		case 0x02:
			sz = 7
		case 0x06:
			sz = 9
		case 0x0e:
			sz = 12
		case 0x0f:
			sz = 32
		}

		bits, err := it.br.readBits(int(sz))
		if err != nil {
			it.err = err
			return false
		}

		if sz == 32 && bits == 0xffffffff {
			it.finished = true
			return false
		}

		dod := decodeZigZag32(uint32(int64(bits) + 1))
		it.tDelta += uint32(dod)
	}

	it.t += uint64(it.tDelta)

	val, err := it.br.readUntilZero(2)
	if err != nil {
		it.err = err
		return false
	}

	switch val {
	case 3:
		bits, err := it.br.readBits(6)
		if err != nil {
			it.err = err
			return false
		}
		it.leading = uint8(bits)

		bits, err = it.br.readBits(6)
		if err != nil {
			it.err = err
			return false
		}
		it.trailing = 64 - (uint8(bits) + 1) - it.leading

		fallthrough
	case 2:
		bits, err := it.br.readBits(int(64 - it.leading - it.trailing))
		if err != nil {
			it.err = err
			return false
		}
		it.val = math.Float64frombits(math.Float64bits(it.val) ^ (bits << it.trailing))
	}

	return true
}

// Values at the current iterator position
func (it *Iter) Values() (uint64, float64) {
	return it.t, it.val
}

// Err error at the current iterator position
func (it *Iter) Err() error {
	return it.err
}

type errMarshal struct {
	w   io.Writer
	r   io.Reader
	err error
}

func (em *errMarshal) write(t interface{}) {
	if em.err != nil {
		return
	}
	em.err = binary.Write(em.w, binary.BigEndian, t)
}

func (em *errMarshal) read(t interface{}) {
	if em.err != nil {
		return
	}
	em.err = binary.Read(em.r, binary.BigEndian, t)
}

// MarshalBinary implements the encoding.BinaryMarshaler interface
func (s *Series) MarshalBinary() ([]byte, error) {
	buf := new(bytes.Buffer)
	em := &errMarshal{w: buf}
	em.write(s.T0)
	em.write(s.leading)
	em.write(s.t)
	em.write(s.tDelta)
	em.write(s.trailing)
	em.write(s.val)
	bStream, err := s.bw.MarshalBinary()
	if err != nil {
		return nil, err
	}
	em.write(bStream)
	if em.err != nil {
		return nil, em.err
	}
	return buf.Bytes(), nil
}

// UnmarshalBinary implements the encoding.BinaryUnmarshaler interface
func (s *Series) UnmarshalBinary(b []byte) error {
	buf := bytes.NewReader(b)
	em := &errMarshal{r: buf}
	em.read(&s.T0)
	em.read(&s.leading)
	em.read(&s.t)
	em.read(&s.tDelta)
	em.read(&s.trailing)
	em.read(&s.val)
	outBuf := make([]byte, buf.Len())
	em.read(outBuf)
	err := s.bw.UnmarshalBinary(outBuf)
	if err != nil {
		return err
	}
	if em.err != nil {
		return em.err
	}
	return nil
}

// Maps negative values to positive values while going back and
// forth (0 = 0, -1 = 1, 1 = 2, -2 = 3, 2 = 4, -3 = 5, 3 = 6 ...)
// Encodes signed integers into unsigned integers that can be efficiently
// encoded with varint because negative values must be sign-extended to 64 bits to
// be varint encoded, thus always taking 10 bytes on the wire.
//
// Read more: https://gist.github.com/mfuerstenau/ba870a29e16536fdbaba
func encodeZigZag32(n int32) uint32 {
	// Note: The right-shift must be arithmetic which it is in Go.
	return uint32(n>>31) ^ (uint32(n) << 1)
}

func decodeZigZag32(n uint32) int32 {
	return int32((n >> 1) ^ uint32((int32(n&1)<<31)>>31))
}
