package kdbx

import (
	"bytes"
	"math/bits"
	"testing"

	"golang.org/x/crypto/salsa20"
)

// referenceSalsa20Block is a literal copy of the Salsa20 core loop, used to locate
// differences between the incremental stream type and the reference implementation.
func referenceSalsa20Block(nonce []byte, counter uint64, key [32]byte) [64]byte {
	var in [16]byte
	copy(in[:8], nonce)
	for i := 0; i < 8; i++ {
		in[8+i] = byte(counter >> (8 * uint(i)))
	}
	c := []byte("expand 32-byte k")
	load := func(b []byte) uint32 {
		return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
	}
	x := [16]uint32{
		load(c[0:4]), load(key[0:4]), load(key[4:8]), load(key[8:12]), load(key[12:16]),
		load(c[4:8]), load(in[0:4]), load(in[4:8]), load(in[8:12]), load(in[12:16]),
		load(c[8:12]), load(key[16:20]), load(key[20:24]), load(key[24:28]), load(key[28:32]),
		load(c[12:16]),
	}
	state := x
	for i := 0; i < 20; i += 2 {
		qr := func(a, b, cc, d int) {
			x[b] ^= bits.RotateLeft32(x[a]+x[d], 7)
			x[cc] ^= bits.RotateLeft32(x[b]+x[a], 9)
			x[d] ^= bits.RotateLeft32(x[cc]+x[b], 13)
			x[a] ^= bits.RotateLeft32(x[d]+x[cc], 18)
		}
		qr(0, 4, 8, 12)
		qr(5, 9, 13, 1)
		qr(10, 14, 2, 6)
		qr(15, 3, 7, 11)
		qr(0, 1, 2, 3)
		qr(5, 6, 7, 4)
		qr(10, 11, 8, 9)
		qr(15, 12, 13, 14)
	}
	var out [64]byte
	for i := 0; i < 16; i++ {
		v := x[i] + state[i]
		out[i*4] = byte(v)
		out[i*4+1] = byte(v >> 8)
		out[i*4+2] = byte(v >> 16)
		out[i*4+3] = byte(v >> 24)
	}
	return out
}

func TestReferenceSalsa20BlockAgainstXCrypto(t *testing.T) {
	var key [32]byte
	for i := range key {
		key[i] = byte(i * 7)
	}
	nonce := []byte{0xE8, 0x30, 0x09, 0x4B, 0x97, 0x20, 0x5D, 0x2A}
	zero := make([]byte, 64)
	want := make([]byte, 64)
	salsa20.XORKeyStream(want, zero, nonce, &key)
	got := referenceSalsa20Block(nonce, 0, key)
	if !bytes.Equal(got[:], want) {
		t.Fatalf("reference core differs from x/crypto\n got: %x\nwant: %x", got[:32], want[:32])
	}
	s := &salsa20Stream{key: key}
	copy(s.nonce[:], nonce)
	s.refill()
	if !bytes.Equal(s.buf[:], got[:]) {
		t.Fatalf("stream type differs from the reference core\n got: %x\nwant: %x", s.buf[:32], got[:32])
	}
}

// The incremental Salsa20 implementation used for KDBX 3.1 protected values must match
// the reference implementation, including when the stream is consumed in small chunks.
func TestSalsa20StreamMatchesXCrypto(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i * 7)
	}
	nonce := []byte{0xE8, 0x30, 0x09, 0x4B, 0x97, 0x20, 0x5D, 0x2A}

	plain := make([]byte, 300)
	for i := range plain {
		plain[i] = byte(i)
	}
	want := make([]byte, len(plain))
	var k [32]byte
	copy(k[:], key)
	salsa20.XORKeyStream(want, plain, nonce, &k)

	// Whole buffer at once.
	gotFull := make([]byte, len(plain))
	s2 := &salsa20Stream{key: k, used: 64}
	copy(s2.nonce[:], nonce)
	s2.XORKeyStream(gotFull, plain)
	if !bytes.Equal(gotFull, want) {
		t.Fatalf("salsa20 keystream does not match x/crypto\n got: %x\nwant: %x", gotFull[:32], want[:32])
	}

	// Deliberately uneven chunks must produce exactly the same keystream.
	gotChunked := make([]byte, len(plain))
	s3 := &salsa20Stream{key: k, used: 64}
	copy(s3.nonce[:], nonce)
	offset := 0
	for _, n := range []int{1, 3, 64, 5, 100, 27, 100} {
		if offset+n > len(plain) {
			n = len(plain) - offset
		}
		if n <= 0 {
			break
		}
		s3.XORKeyStream(gotChunked[offset:offset+n], plain[offset:offset+n])
		offset += n
	}
	if offset < len(plain) {
		s3.XORKeyStream(gotChunked[offset:], plain[offset:])
	}
	if !bytes.Equal(gotChunked, want) {
		t.Fatalf("chunked salsa20 keystream does not match x/crypto\n got: %x\nwant: %x", gotChunked[:32], want[:32])
	}
}
