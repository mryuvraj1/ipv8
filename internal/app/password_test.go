package app

import (
	"encoding/hex"
	"testing"
)

func TestPBKDF2SHA256Vector(t *testing.T) {
	got := hex.EncodeToString(pbkdf2SHA256([]byte("password"), []byte("salt"), 1, 32))
	want := "120fb6cffcf8b32c43e7225256c4f837a86548c92ccc35480805987cb70be17b"
	if got != want {
		t.Fatalf("PBKDF2 vector = %s, want %s", got, want)
	}
}
