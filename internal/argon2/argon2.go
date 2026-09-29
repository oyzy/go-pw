// Package argon2 implements the Argon2 key derivation function (RFC 9106) for the
// Argon2d, Argon2i and Argon2id variants.
//
// golang.org/x/crypto only exposes Argon2i and Argon2id, but KeePass 2 defaults to
// Argon2d, so the full algorithm is implemented here. The implementation is verified
// against both the x/crypto implementation and real KeePass databases in the tests.
package argon2

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"
	"sync"

	"golang.org/x/crypto/blake2b"
)

// Type selects the Argon2 variant.
type Type uint32

// The variants defined by RFC 9106.
const (
	TypeD  Type = 0 // data dependent addressing
	TypeI  Type = 1 // data independent addressing
	TypeID Type = 2 // hybrid: independent for the first half of the first pass
)

// Version 1.3 is the only version KeePass 2 writes; 1.0 is accepted for older files.
const (
	Version10 uint32 = 0x10
	Version13 uint32 = 0x13
)

// Config describes one Argon2 invocation.
type Config struct {
	Type    Type
	Version uint32
	// Time is the number of passes.
	Time uint32
	// Memory is the memory size in KiB, exactly like the "M" parameter of KeePass
	// divided by 1024.
	Memory uint32
	// Threads is the degree of parallelism ("P" in KeePass).
	Threads uint8
	// KeyLen is the desired output length in bytes.
	KeyLen uint32
	Salt   []byte
	// Secret is the optional key ("K").
	Secret []byte
	// Data is the optional associated data ("A").
	Data []byte
}

// DeriveKey computes the Argon2 tag for the given password.
func DeriveKey(password []byte, cfg Config) ([]byte, error) {
	if cfg.Time < 1 {
		return nil, errors.New("argon2: number of passes must be at least 1")
	}
	if cfg.Threads < 1 {
		return nil, errors.New("argon2: parallelism must be at least 1")
	}
	if cfg.KeyLen < 4 {
		return nil, errors.New("argon2: output length must be at least 4 bytes")
	}
	if len(cfg.Salt) < 8 {
		return nil, errors.New("argon2: salt must be at least 8 bytes")
	}
	if cfg.Version != Version13 {
		return nil, fmt.Errorf("argon2: unsupported version 0x%x", cfg.Version)
	}

	const syncPoints = 4
	lanes := uint32(cfg.Threads)
	// The initial hash uses the memory parameter as requested by the caller, while the
	// block array is sized with the rounded down value.
	requestedMemory := cfg.Memory
	memory := requestedMemory
	if memory < 8*lanes {
		memory = 8 * lanes
	}
	segmentLength := memory / (lanes * syncPoints)
	if segmentLength < 2 {
		segmentLength = 2
	}
	laneLength := segmentLength * syncPoints
	memory = laneLength * lanes

	h0 := initialHash(password, cfg, requestedMemory)

	// B[i][j] is lane i, block j.
	mem := make([]block, memory)
	for lane := uint32(0); lane < lanes; lane++ {
		var seed [72]byte
		copy(seed[:64], h0[:])
		binary.LittleEndian.PutUint32(seed[64:68], 0)
		binary.LittleEndian.PutUint32(seed[68:72], lane)
		hashPrimeBlock(&mem[lane*laneLength+0], seed[:])

		binary.LittleEndian.PutUint32(seed[64:68], 1)
		hashPrimeBlock(&mem[lane*laneLength+1], seed[:])
	}

	var wg sync.WaitGroup
	for pass := uint32(0); pass < cfg.Time; pass++ {
		for slice := uint32(0); slice < syncPoints; slice++ {
			wg.Add(int(lanes))
			for lane := uint32(0); lane < lanes; lane++ {
				go func(pass, slice, lane uint32) {
					defer wg.Done()
					fillSegment(mem, cfg, pass, slice, lane, laneLength, segmentLength, memory)
				}(pass, slice, lane)
			}
			wg.Wait()
		}
	}

	// XOR the last column of every lane, then hash once more.
	var final block
	for lane := uint32(0); lane < lanes; lane++ {
		last := mem[lane*laneLength+laneLength-1]
		for i := range final {
			final[i] ^= last[i]
		}
	}
	out := make([]byte, cfg.KeyLen)
	hashPrime(out, blockBytes(&final))
	return out, nil
}

// Key is a convenience wrapper that derives a key the way x/crypto's argon2.IDKey does.
func Key(password, salt []byte, time, memory uint32, threads uint8, keyLen uint32) []byte {
	out, err := DeriveKey(password, Config{
		Type:    TypeI,
		Version: Version13,
		Time:    time,
		Memory:  memory,
		Threads: threads,
		KeyLen:  keyLen,
		Salt:    salt,
	})
	if err != nil {
		panic("argon2: " + err.Error())
	}
	return out
}

// IDKey mirrors x/crypto's argon2.IDKey.
func IDKey(password, salt []byte, time, memory uint32, threads uint8, keyLen uint32) []byte {
	out, err := DeriveKey(password, Config{
		Type:    TypeID,
		Version: Version13,
		Time:    time,
		Memory:  memory,
		Threads: threads,
		KeyLen:  keyLen,
		Salt:    salt,
	})
	if err != nil {
		panic("argon2: " + err.Error())
	}
	return out
}

func initialHash(password []byte, cfg Config, memory uint32) [64]byte {
	h, err := blake2b.New512(nil)
	if err != nil {
		panic(err)
	}
	var buf [4]byte
	writeU32 := func(v uint32) {
		binary.LittleEndian.PutUint32(buf[:], v)
		h.Write(buf[:])
	}
	writeU32(uint32(cfg.Threads))
	writeU32(cfg.KeyLen)
	writeU32(memory)
	writeU32(cfg.Time)
	writeU32(cfg.Version)
	writeU32(uint32(cfg.Type))
	writeU32(uint32(len(password)))
	h.Write(password)
	writeU32(uint32(len(cfg.Salt)))
	h.Write(cfg.Salt)
	writeU32(uint32(len(cfg.Secret)))
	h.Write(cfg.Secret)
	writeU32(uint32(len(cfg.Data)))
	h.Write(cfg.Data)

	var out [64]byte
	copy(out[:], h.Sum(nil))
	return out
}

// fillSegment computes one segment of one lane.
func fillSegment(mem []block, cfg Config, pass, slice, lane, laneLength, segmentLength, memory uint32) {
	const syncPoints = 4
	lanes := uint32(cfg.Threads)
	typeVariant := cfg.Type
	// Argon2id behaves like Argon2i for the first pass, first two slices.
	dataIndependent := typeVariant == TypeI ||
		(typeVariant == TypeID && pass == 0 && slice < syncPoints/2)

	var (
		addressBlock block
		inputBlock   block
		zeroBlock    block
	)
	pseudoRandom := func(indexInSegment uint32) uint64 {
		if dataIndependent {
			if indexInSegment%128 == 0 {
				inputBlock[0] = uint64(pass)
				inputBlock[1] = uint64(lane)
				inputBlock[2] = uint64(slice)
				inputBlock[3] = uint64(memory)
				inputBlock[4] = uint64(cfg.Time)
				inputBlock[5] = uint64(cfg.Type)
				inputBlock[6]++
				compress(&zeroBlock, &inputBlock, &addressBlock, false)
				tmp := addressBlock
				compress(&zeroBlock, &tmp, &addressBlock, false)
			}
			return addressBlock[indexInSegment%128]
		}
		prev := prevBlockIndex(slice, indexInSegment, segmentLength, laneLength)
		return mem[lane*laneLength+prev][0]
	}

	startIndex := uint32(0)
	if pass == 0 && slice == 0 {
		// The first two blocks of every lane are already initialised.
		startIndex = 2
		// Keep the address counter in step with the reference implementation.
		if dataIndependent {
			_ = pseudoRandom(0)
		}
	}

	for index := startIndex; index < segmentLength; index++ {
		prev := prevBlockIndex(slice, index, segmentLength, laneLength)
		random := pseudoRandom(index)
		j1 := uint32(random & 0xFFFFFFFF)
		j2 := uint32(random >> 32)

		refLane := lane
		if pass == 0 && slice == 0 {
			// Only the current lane may be referenced in the very first segment.
			refLane = lane
		} else {
			refLane = j2 % lanes
		}
		sameLane := refLane == lane
		refIndex := indexAlpha(pass, slice, index, j1, sameLane, laneLength, segmentLength)

		prevBlock := &mem[lane*laneLength+prev]
		refBlock := &mem[refLane*laneLength+refIndex]
		cur := &mem[lane*laneLength+slice*segmentLength+index]
		compress(prevBlock, refBlock, cur, pass > 0 && cfg.Version == Version13)
	}
}

func prevBlockIndex(slice, index, segmentLength, laneLength uint32) uint32 {
	if index == 0 {
		if slice == 0 {
			return laneLength - 1
		}
		return slice*segmentLength - 1
	}
	return slice*segmentLength + index - 1
}

// indexAlpha implements the "index_alpha" function of the reference implementation.
// The intermediate values must be 64 bit wide: squaring a 32 bit word would otherwise
// overflow to zero and produce a completely different reference block.
func indexAlpha(pass, slice, index, pseudoRand uint32, sameLane bool, laneLength, segmentLength uint32) uint32 {
	const syncPoints = 4
	var refAreaSize, startPos uint32
	if pass == 0 {
		if slice == 0 {
			refAreaSize = index - 1
		} else if sameLane {
			refAreaSize = slice*segmentLength + index - 1
		} else {
			refAreaSize = slice * segmentLength
			if index == 0 {
				refAreaSize--
			}
		}
	} else {
		if sameLane {
			refAreaSize = laneLength - segmentLength + index - 1
		} else {
			refAreaSize = laneLength - segmentLength
			if index == 0 {
				refAreaSize--
			}
		}
	}
	relative := uint64(pseudoRand)
	relative = (relative * relative) >> 32
	relative = uint64(refAreaSize) - 1 - ((uint64(refAreaSize) * relative) >> 32)
	if pass != 0 && slice != syncPoints-1 {
		startPos = (slice + 1) * segmentLength
	}
	return uint32((uint64(startPos) + relative) % uint64(laneLength))
}

// --- the compression function G and the permutation P -------------------------

// compress implements the compression function G(x, y) = P(x ^ y) ^ (x ^ y). When
// withXor is set (second and later passes, as required by RFC 9106 §3.5) the previous
// content of the destination block is XORed in afterwards.
func compress(x, y, out *block, withXor bool) {
	var r, tmp block
	for i := range r {
		r[i] = x[i] ^ y[i]
	}
	tmp = r
	permute(&tmp)
	for i := range out {
		v := tmp[i] ^ r[i]
		if withXor {
			v ^= out[i]
		}
		out[i] = v
	}
}

func permute(b *block) {
	for i := 0; i < 8; i++ {
		round(b[16*i : 16*i+16])
	}
	for i := 0; i < 8; i++ {
		var v [16]uint64
		idx := [16]int{2 * i, 2*i + 1, 2*i + 16, 2*i + 17, 2*i + 32, 2*i + 33, 2*i + 48, 2*i + 49,
			2*i + 64, 2*i + 65, 2*i + 80, 2*i + 81, 2*i + 96, 2*i + 97, 2*i + 112, 2*i + 113}
		for k, j := range idx {
			v[k] = b[j]
		}
		round(v[:])
		for k, j := range idx {
			b[j] = v[k]
		}
	}
}

func round(v []uint64) {
	gb(&v[0], &v[4], &v[8], &v[12])
	gb(&v[1], &v[5], &v[9], &v[13])
	gb(&v[2], &v[6], &v[10], &v[14])
	gb(&v[3], &v[7], &v[11], &v[15])
	gb(&v[0], &v[5], &v[10], &v[15])
	gb(&v[1], &v[6], &v[11], &v[12])
	gb(&v[2], &v[7], &v[8], &v[13])
	gb(&v[3], &v[4], &v[9], &v[14])
}

func gb(a, b, c, d *uint64) {
	mix(a, b, d, 32)
	mix(c, d, b, 24)
	mix(a, b, d, 16)
	mix(c, d, b, 63)
}

func mix(a, b, d *uint64, rot uint) {
	*a = *a + *b + 2*uint64(uint32(*a))*uint64(uint32(*b))
	*d = bits.RotateLeft64(*d^*a, -int(rot))
}

// block is the 1024 byte working unit of Argon2.
type block [128]uint64

func blockBytes(b *block) []byte {
	out := make([]byte, 1024)
	for i, w := range b {
		binary.LittleEndian.PutUint64(out[i*8:], w)
	}
	return out
}

// hashPrimeBlock fills a whole block using H'^1024.
func hashPrimeBlock(dst *block, in []byte) {
	buf := make([]byte, 1024)
	hashPrime(buf, in)
	for i := 0; i < len(dst); i++ {
		dst[i] = binary.LittleEndian.Uint64(buf[i*8:])
	}
}

// hashPrime implements the variable length hash function H' of RFC 9106.
func hashPrime(out []byte, in []byte) {
	outLen := len(out)
	var lenBytes [4]byte
	binary.LittleEndian.PutUint32(lenBytes[:], uint32(outLen))

	if outLen <= 64 {
		h, err := blake2b.New(outLen, nil)
		if err != nil {
			panic(err)
		}
		h.Write(lenBytes[:])
		h.Write(in)
		copy(out, h.Sum(nil))
		return
	}

	h, err := blake2b.New512(nil)
	if err != nil {
		panic(err)
	}
	h.Write(lenBytes[:])
	h.Write(in)
	v := h.Sum(nil)

	chunks := (outLen+31)/32 - 2
	pos := 0
	for i := 0; i < chunks; i++ {
		copy(out[pos:], v[:32])
		pos += 32
		h2, err := blake2b.New512(nil)
		if err != nil {
			panic(err)
		}
		h2.Write(v)
		v = h2.Sum(nil)
	}
	copy(out[pos:], v)
}
