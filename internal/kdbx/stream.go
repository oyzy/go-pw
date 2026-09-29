package kdbx

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"fmt"
	"math/bits"

	"golang.org/x/crypto/chacha20"
)

// Inner random stream identifiers.
const (
	InnerStreamNone     = 0
	InnerStreamArcFour  = 1
	InnerStreamSalsa20  = 2
	InnerStreamChaCha20 = 3
)

// randomStream produces the keystream used to obfuscate protected values.
type randomStream interface {
	XORKeyStream(dst, src []byte)
}

// innerStreamIV is the fixed nonce used by both inner stream ciphers, taken from the
// KeePass sources. Salsa20 uses the first 8 bytes, ChaCha20 the whole 12 bytes.
var innerStreamIV = []byte{0xE8, 0x30, 0x09, 0x4B, 0x97, 0x20, 0x5D, 0x2A, 0x00, 0x00, 0x00, 0x00}

// newRandomStream creates the inner stream for the given identifier and key.
func newRandomStream(id uint32, key []byte) (randomStream, error) {
	switch id {
	case InnerStreamNone:
		return nopStream{}, nil
	case InnerStreamSalsa20:
		var k [32]byte
		sum := sha256.Sum256(key)
		copy(k[:], sum[:])
		s := &salsa20Stream{key: k}
		copy(s.nonce[:], innerStreamIV[:8])
		// Mark the buffer as empty so that the first call fills it.
		s.used = len(s.buf)
		return s, nil
	case InnerStreamChaCha20:
		// KeePass derives both the key and the IV from the SHA-512 of the stream key:
		// the first 32 bytes become the key, the next twelve bytes the nonce.
		sum := sha512.Sum512(key)
		c, err := chacha20.NewUnauthenticatedCipher(sum[0:32], sum[32:44])
		if err != nil {
			return nil, err
		}
		return c, nil
	}
	return nil, fmt.Errorf("kdbx: unsupported inner random stream algorithm %d", id)
}

type nopStream struct{}

func (nopStream) XORKeyStream(dst, src []byte) { copy(dst, src) }

// salsa20Stream is a Salsa20 keystream generator that can be consumed in arbitrary
// chunk sizes, which XML parsing requires. x/crypto does not expose an incremental
// Salsa20 API, so the cipher core is implemented here (and checked against
// x/crypto/salsa20 in the tests).
type salsa20Stream struct {
	key     [32]byte
	nonce   [8]byte
	counter uint64
	buf     [64]byte
	used    int
}

func (s *salsa20Stream) XORKeyStream(dst, src []byte) {
	if len(dst) < len(src) {
		panic("kdbx: output buffer smaller than input")
	}
	for len(src) > 0 {
		if s.used == len(s.buf) {
			s.refill()
		}
		n := len(s.buf) - s.used
		if n > len(src) {
			n = len(src)
		}
		for i := 0; i < n; i++ {
			dst[i] = src[i] ^ s.buf[s.used+i]
		}
		s.used += n
		dst = dst[n:]
		src = src[n:]
	}
}

func (s *salsa20Stream) refill() {
	var state [16]uint32
	const (
		c0 = 0x61707865 // "expa"
		c1 = 0x3320646e // "nd 3"
		c2 = 0x79622d32 // "2-by"
		c3 = 0x6b206574 // "te k"
	)
	state[0] = c0
	for i := 0; i < 4; i++ {
		state[1+i] = binary.LittleEndian.Uint32(s.key[i*4:])
		state[11+i] = binary.LittleEndian.Uint32(s.key[16+i*4:])
	}
	state[5] = c1
	state[10] = c2
	state[15] = c3
	state[6] = binary.LittleEndian.Uint32(s.nonce[0:4])
	state[7] = binary.LittleEndian.Uint32(s.nonce[4:8])
	state[8] = uint32(s.counter)
	state[9] = uint32(s.counter >> 32)
	s.counter++

	x := state
	for i := 0; i < 10; i++ {
		salsaQuarterRound(&x[0], &x[4], &x[8], &x[12])
		salsaQuarterRound(&x[5], &x[9], &x[13], &x[1])
		salsaQuarterRound(&x[10], &x[14], &x[2], &x[6])
		salsaQuarterRound(&x[15], &x[3], &x[7], &x[11])
		salsaQuarterRound(&x[0], &x[1], &x[2], &x[3])
		salsaQuarterRound(&x[5], &x[6], &x[7], &x[4])
		salsaQuarterRound(&x[10], &x[11], &x[8], &x[9])
		salsaQuarterRound(&x[15], &x[12], &x[13], &x[14])
	}
	for i := 0; i < 16; i++ {
		binary.LittleEndian.PutUint32(s.buf[i*4:], x[i]+state[i])
	}
	s.used = 0
}

func salsaQuarterRound(a, b, c, d *uint32) {
	*b ^= bits.RotateLeft32(*a+*d, 7)
	*c ^= bits.RotateLeft32(*b+*a, 9)
	*d ^= bits.RotateLeft32(*c+*b, 13)
	*a ^= bits.RotateLeft32(*d+*c, 18)
}
