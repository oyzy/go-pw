package dbdoc

import (
	"crypto/aes"
	"crypto/sha256"
	"errors"
	"fmt"

	"gokeep/internal/argon2"
)

// Well known key derivation function UUIDs.
var (
	// KDFAES is the AES-KDF (a.k.a. "AES Transform") used by all KeePass 2 versions.
	KDFAES = MustParseUUID("C9D9F39A628A4460BF740D08C18A4FEA")
	// KDFArgon2d is the KeePass 2 default since 2.35.
	KDFArgon2d = MustParseUUID("EF636DDF8C29444B91F7A9A403E30A0C")
	// KDFArgon2id is offered as an alternative.
	KDFArgon2id = MustParseUUID("9E298B1956DB4773B23DFC3EC6F0A1E6")
)

// Parameter names inside the KDF variant dictionary, identical to KeePassLib.
const (
	KdfParamSeed        = "S"
	KdfParamRounds      = "R"
	KdfParamParallelism = "P"
	KdfParamMemory      = "M"
	KdfParamIterations  = "I"
	KdfParamVersion     = "V"
	KdfParamSecretKey   = "K"
	KdfParamAssocData   = "A"
	KdfParamUUID        = "$UUID"
)

// KDF resource limits prevent untrusted headers from forcing excessive work.
const (
	MaxArgon2Memory      uint64 = uint64(argon2.MaxMemoryKiB) * 1024
	MaxArgon2Iterations  uint64 = uint64(argon2.MaxPasses)
	MaxArgon2Parallelism uint32 = uint32(argon2.MaxThreads)
	MaxAESKDFRounds      uint64 = 200_000_000
)

// KDFParams wraps the variant dictionary stored in the KDBX 4 header field 11.
// Unknown parameters are preserved on save so that databases written by other
// applications survive a round trip.
type KDFParams struct {
	Dict *VariantDict
}

// NewKDFParams creates KDF parameters for the given KDF UUID.
func NewKDFParams(uuid UUID) *KDFParams {
	d := NewVariantDict()
	d.SetBytes(KdfParamUUID, uuid[:])
	return &KDFParams{Dict: d}
}

// UUID returns the KDF identifier.
func (p *KDFParams) UUID() UUID {
	if p == nil || p.Dict == nil {
		return UUID{}
	}
	b := p.Dict.GetBytes(KdfParamUUID)
	if len(b) != 16 {
		return UUID{}
	}
	var u UUID
	copy(u[:], b)
	return u
}

// Name returns a human readable KDF name.
func (p *KDFParams) Name() string {
	switch p.UUID() {
	case KDFAES:
		return "AES-KDF"
	case KDFArgon2d:
		return "Argon2d"
	case KDFArgon2id:
		return "Argon2id"
	}
	return "Unknown"
}

// Transform derives the 32 byte transformed key from the composite key.
func (p *KDFParams) Transform(composite [32]byte) ([32]byte, error) {
	var out [32]byte
	if err := p.Validate(); err != nil {
		return out, err
	}
	switch p.UUID() {
	case KDFAES:
		seed := p.Dict.GetBytes(KdfParamSeed)
		rounds := p.Dict.GetUint64(KdfParamRounds, 0)
		return aesTransformKey(seed, rounds, composite), nil

	case KDFArgon2d, KDFArgon2id:
		salt := p.Dict.GetBytes(KdfParamSeed)
		version := uint32(p.Dict.GetUint64(KdfParamVersion, 0x13))
		memory := p.Dict.GetUint64(KdfParamMemory, 64*1024*1024)
		iterations := p.Dict.GetUint64(KdfParamIterations, 2)
		parallelism := p.Dict.GetUint64(KdfParamParallelism, 2)
		cfg := argon2.Config{
			Version: version,
			// KeePass stores M in bytes; Argon2 wants KiB.
			Memory:  uint32(memory / 1024),
			Time:    uint32(iterations),
			Threads: uint8(parallelism),
			KeyLen:  32,
			Salt:    salt,
			Secret:  p.Dict.GetBytes(KdfParamSecretKey),
			Data:    p.Dict.GetBytes(KdfParamAssocData),
		}
		if p.UUID() == KDFArgon2d {
			cfg.Type = argon2.TypeD
		} else {
			cfg.Type = argon2.TypeID
		}
		key, err := argon2.DeriveKey(composite[:], cfg)
		if err != nil {
			return out, fmt.Errorf("gokeep: argon2: %w", err)
		}
		copy(out[:], key)
		return out, nil
	}
	return out, fmt.Errorf("gokeep: unsupported key derivation function %s", p.UUID())
}

// aesTransformKey implements the classic AES-KDF: the 32 byte composite key is
// encrypted `rounds` times with AES-256-ECB using the transform seed as the key.
func aesTransformKey(seed []byte, rounds uint64, composite [32]byte) [32]byte {
	block, err := aes.NewCipher(seed)
	if err != nil {
		// The seed is checked to be 32 bytes by the caller.
		panic(err)
	}
	var key [32]byte
	copy(key[:], composite[:])
	for i := uint64(0); i < rounds; i++ {
		block.Encrypt(key[0:16], key[0:16])
		block.Encrypt(key[16:32], key[16:32])
	}
	return sha256.Sum256(key[:])
}

// DefaultKDFParams returns the KDF parameters KeePass 2 uses for new databases:
// Argon2d with 64 MiB of memory, 2 iterations and 2 lanes.
func DefaultKDFParams() *KDFParams {
	salt := make([]byte, 32)
	mustRand(salt)
	p := NewKDFParams(KDFArgon2d)
	p.Dict.SetBytes(KdfParamSeed, salt)
	p.Dict.SetUint32(KdfParamParallelism, 2)
	p.Dict.SetUint64(KdfParamMemory, 64*1024*1024)
	p.Dict.SetUint64(KdfParamIterations, 2)
	p.Dict.SetUint32(KdfParamVersion, 0x13)
	return p
}

// DefaultAESKDFParams returns AES-KDF parameters equivalent to KeePass defaults for
// AES-KDF (60 million rounds), the configuration used before Argon2 existed.
func DefaultAESKDFParams() *KDFParams {
	seed := make([]byte, 32)
	mustRand(seed)
	p := NewKDFParams(KDFAES)
	p.Dict.SetUint64(KdfParamRounds, 60000)
	p.Dict.SetBytes(KdfParamSeed, seed)
	return p
}

// Validate checks that the parameters are usable.
func (p *KDFParams) Validate() error {
	if p == nil || p.Dict == nil {
		return errors.New("gokeep: missing key derivation parameters")
	}
	switch p.UUID() {
	case KDFAES:
		if len(p.Dict.GetBytes(KdfParamSeed)) != 32 {
			return errors.New("gokeep: AES-KDF parameters need a 32 byte seed")
		}
		rounds := p.Dict.GetUint64(KdfParamRounds, 0)
		if rounds == 0 {
			return errors.New("gokeep: AES-KDF rounds must be at least 1")
		}
		if rounds > MaxAESKDFRounds {
			return fmt.Errorf("gokeep: AES-KDF rounds must not exceed %d", MaxAESKDFRounds)
		}
	case KDFArgon2d, KDFArgon2id:
		if len(p.Dict.GetBytes(KdfParamSeed)) < 8 {
			return errors.New("gokeep: Argon2 parameters need a salt of at least 8 bytes")
		}
		version := p.Dict.GetUint64(KdfParamVersion, 0x13)
		if version != 0x13 {
			return fmt.Errorf("gokeep: Argon2 version 0x%x is not supported (only 0x13)", version)
		}
		memory := p.Dict.GetUint64(KdfParamMemory, 64*1024*1024)
		iterations := p.Dict.GetUint64(KdfParamIterations, 2)
		parallelism := p.Dict.GetUint64(KdfParamParallelism, 2)
		if parallelism == 0 {
			return errors.New("gokeep: Argon2 parallelism must be at least 1")
		}
		if parallelism > uint64(MaxArgon2Parallelism) {
			return fmt.Errorf("gokeep: Argon2 parallelism must not exceed %d", MaxArgon2Parallelism)
		}
		if iterations == 0 {
			return errors.New("gokeep: Argon2 iterations must be at least 1")
		}
		if iterations > MaxArgon2Iterations {
			return fmt.Errorf("gokeep: Argon2 iterations must not exceed %d", MaxArgon2Iterations)
		}
		if memory > MaxArgon2Memory {
			return fmt.Errorf("gokeep: Argon2 memory must not exceed %d bytes", MaxArgon2Memory)
		}
		minMemory := uint64(8*1024) * parallelism
		if memory < minMemory {
			return fmt.Errorf("gokeep: Argon2 memory must be at least %d bytes", minMemory)
		}
	default:
		return fmt.Errorf("gokeep: unsupported key derivation function %s", p.UUID())
	}
	return nil
}
