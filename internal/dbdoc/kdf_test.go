package dbdoc

import (
	"bytes"
	"testing"
)

func TestKDFParamsValidateLimits(t *testing.T) {
	argon := NewKDFParams(KDFArgon2id)
	argon.Dict.SetBytes(KdfParamSeed, bytes.Repeat([]byte{1}, 32))
	argon.Dict.SetUint64(KdfParamMemory, 64*1024*1024)
	argon.Dict.SetUint64(KdfParamIterations, 2)
	argon.Dict.SetUint64(KdfParamParallelism, 2)
	argon.Dict.SetUint32(KdfParamVersion, 0x13)
	if err := argon.Validate(); err != nil {
		t.Fatalf("valid Argon2 parameters rejected: %v", err)
	}

	aes := NewKDFParams(KDFAES)
	aes.Dict.SetBytes(KdfParamSeed, bytes.Repeat([]byte{2}, 32))
	aes.Dict.SetUint64(KdfParamRounds, 60000)
	if err := aes.Validate(); err != nil {
		t.Fatalf("valid AES-KDF parameters rejected: %v", err)
	}

	paranoid := NewKDFParams(KDFArgon2id)
	paranoid.Dict.SetBytes(KdfParamSeed, bytes.Repeat([]byte{3}, 32))
	paranoid.Dict.SetUint64(KdfParamMemory, MaxArgon2Memory)
	paranoid.Dict.SetUint64(KdfParamIterations, MaxArgon2Iterations)
	paranoid.Dict.SetUint64(KdfParamParallelism, uint64(MaxArgon2Parallelism))
	paranoid.Dict.SetUint32(KdfParamVersion, 0x13)
	if err := paranoid.Validate(); err != nil {
		t.Fatalf("maximum supported Argon2 parameters rejected: %v", err)
	}

	clone := func(p *KDFParams) *KDFParams {
		out := NewKDFParams(p.UUID())
		for _, key := range p.Dict.Keys() {
			switch key {
			case KdfParamSeed:
				out.Dict.SetBytes(key, p.Dict.GetBytes(key))
			case KdfParamRounds, KdfParamMemory, KdfParamIterations:
				out.Dict.SetUint64(key, p.Dict.GetUint64(key, 0))
			case KdfParamParallelism, KdfParamVersion:
				out.Dict.SetUint32(key, p.Dict.GetUint32(key, 0))
			}
		}
		return out
	}

	cases := []struct {
		name   string
		params func() *KDFParams
	}{
		{"argon memory too large", func() *KDFParams {
			p := clone(argon)
			p.Dict.SetUint64(KdfParamMemory, MaxArgon2Memory+1024)
			return p
		}},
		{"argon memory too small", func() *KDFParams {
			p := clone(argon)
			p.Dict.SetUint64(KdfParamMemory, 8*1024*2-1)
			return p
		}},
		{"argon iterations zero", func() *KDFParams {
			p := clone(argon)
			p.Dict.SetUint64(KdfParamIterations, 0)
			return p
		}},
		{"argon iterations too large", func() *KDFParams {
			p := clone(argon)
			p.Dict.SetUint64(KdfParamIterations, MaxArgon2Iterations+1)
			return p
		}},
		{"argon parallelism zero", func() *KDFParams {
			p := clone(argon)
			p.Dict.SetUint64(KdfParamParallelism, 0)
			return p
		}},
		{"argon parallelism too large", func() *KDFParams {
			p := clone(argon)
			p.Dict.SetUint64(KdfParamParallelism, uint64(MaxArgon2Parallelism)+1)
			return p
		}},
		{"argon parallelism cannot be truncated", func() *KDFParams {
			p := clone(argon)
			p.Dict.SetUint64(KdfParamParallelism, 1<<32+2)
			return p
		}},
		{"argon version unsupported", func() *KDFParams {
			p := clone(argon)
			p.Dict.SetUint32(KdfParamVersion, 0x10)
			return p
		}},
		{"aes rounds zero", func() *KDFParams {
			p := clone(aes)
			p.Dict.SetUint64(KdfParamRounds, 0)
			return p
		}},
		{"aes rounds too large", func() *KDFParams {
			p := clone(aes)
			p.Dict.SetUint64(KdfParamRounds, MaxAESKDFRounds+1)
			return p
		}},
		{"unknown KDF", func() *KDFParams {
			return NewKDFParams(UUID{})
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.params().Validate(); err == nil {
				t.Fatal("unsafe KDF parameters were accepted")
			}
		})
	}
}
