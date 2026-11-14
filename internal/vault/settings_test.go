package vault

import (
	"testing"

	"gokeep/internal/dbdoc"
)

func TestSecuritySettingsStayWithinKDFLimits(t *testing.T) {
	v := &Vault{}
	v.applySecuritySettings(DatabaseSettings{
		KDFName:        "Argon2id",
		KDFMemory:      dbdoc.MaxArgon2Memory + 1024,
		KDFIterations:  dbdoc.MaxArgon2Iterations + 1,
		KDFParallelism: dbdoc.MaxArgon2Parallelism + 1,
	})
	if err := v.settings.KDF.Validate(); err != nil {
		t.Fatalf("clamped Argon2 settings are invalid: %v", err)
	}
	if got := v.settings.KDF.Dict.GetUint64(dbdoc.KdfParamMemory, 0); got != dbdoc.MaxArgon2Memory {
		t.Errorf("Argon2 memory = %d, want %d", got, dbdoc.MaxArgon2Memory)
	}
	if got := v.settings.KDF.Dict.GetUint64(dbdoc.KdfParamIterations, 0); got != dbdoc.MaxArgon2Iterations {
		t.Errorf("Argon2 iterations = %d, want %d", got, dbdoc.MaxArgon2Iterations)
	}
	if got := v.settings.KDF.Dict.GetUint32(dbdoc.KdfParamParallelism, 0); got != dbdoc.MaxArgon2Parallelism {
		t.Errorf("Argon2 parallelism = %d, want %d", got, dbdoc.MaxArgon2Parallelism)
	}

	v.applySecuritySettings(DatabaseSettings{
		KDFName:   "AES-KDF",
		KDFRounds: dbdoc.MaxAESKDFRounds + 1,
	})
	if err := v.settings.KDF.Validate(); err != nil {
		t.Fatalf("clamped AES-KDF settings are invalid: %v", err)
	}
	if got := v.settings.KDF.Dict.GetUint64(dbdoc.KdfParamRounds, 0); got != dbdoc.MaxAESKDFRounds {
		t.Errorf("AES-KDF rounds = %d, want %d", got, dbdoc.MaxAESKDFRounds)
	}
}
