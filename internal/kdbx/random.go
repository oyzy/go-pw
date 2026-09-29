package kdbx

import "crypto/rand"

func mustRand(b []byte) {
	if _, err := rand.Read(b); err != nil {
		panic("kdbx: cannot read random bytes: " + err.Error())
	}
}
