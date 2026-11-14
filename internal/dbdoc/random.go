package dbdoc

import "crypto/rand"

func mustRand(b []byte) {
	if _, err := rand.Read(b); err != nil {
		panic("gokeep: cannot read random bytes: " + err.Error())
	}
}
