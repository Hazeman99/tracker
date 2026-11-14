package auth

import (
	"strings"
	"testing"
)

var testParams = Params{Memory: 1024, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}

func TestHashVerify(t *testing.T) {
	h, err := NewHasher(testParams, 2)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := h.Hash("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enc, "$argon2id$v=19$m=1024,t=1,p=1$") {
		t.Fatalf("unexpected encoding %q", enc)
	}
	if !h.Verify("correct horse battery staple", enc) {
		t.Fatal("correct password rejected")
	}
	if h.Verify("correct horse battery stapl", enc) {
		t.Fatal("wrong password accepted")
	}
	for _, bad := range []string{"", "$argon2i$v=19$m=1024,t=1,p=1$a$b", "$argon2id$v=19$m=999999999,t=1,p=1$c2FsdHNhbHQ$a2V5a2V5a2V5a2V5a2V5a2V5"} {
		if h.Verify("x", bad) {
			t.Fatalf("malformed hash %q accepted", bad)
		}
	}
}

func TestDefaultParamsAreOWASP(t *testing.T) {
	if DefaultParams.Memory != 19456 || DefaultParams.Time != 2 || DefaultParams.Threads != 1 {
		t.Fatalf("default params drifted from OWASP recommendation: %+v", DefaultParams)
	}
}

func TestPolicy(t *testing.T) {
	if CheckPolicy("short") == nil {
		t.Fatal("short password accepted")
	}
	if CheckPolicy("ééééééééééé") == nil { // 11 code points
		t.Fatal("11-character password accepted")
	}
	if CheckPolicy("éééééééééééé") != nil { // 12 code points
		t.Fatal("12-character password rejected")
	}
	if CheckPolicy(strings.Repeat("a", MaxPasswordLen+1)) == nil {
		t.Fatal("over-long password accepted")
	}
}

func TestTokens(t *testing.T) {
	tok, hash, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if !WellFormedToken(tok) {
		t.Fatal("issued token not well-formed")
	}
	if len(hash) != 32 || string(hash) != string(HashToken(tok)) {
		t.Fatal("hash mismatch")
	}
	for _, bad := range []string{"", "abc", strings.Repeat("!", len(tok)), tok + "A"} {
		if WellFormedToken(bad) {
			t.Fatalf("%q accepted", bad)
		}
	}
}

func TestIDs(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id := NewID()
		if !ValidID(id) || id[14] != '4' {
			t.Fatalf("bad id %q", id)
		}
		if seen[id] {
			t.Fatal("duplicate id")
		}
		seen[id] = true
	}
	for _, bad := range []string{"", "not-a-uuid", "6F9619FF-8B86-D011-B42D-00C04FC964FF", "6f9619ff-8b86-d011-b42d-00c04fc964f'", "6f9619ff8b86-d011-b42d-00c04fc964ff0"} {
		if ValidID(bad) {
			t.Fatalf("%q accepted", bad)
		}
	}
}
