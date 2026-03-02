package security

import "testing"

func TestRandomTokenReturnsDifferentValues(t *testing.T) {
	left, err := RandomToken(16)
	if err != nil {
		t.Fatalf("RandomToken left: %v", err)
	}
	right, err := RandomToken(16)
	if err != nil {
		t.Fatalf("RandomToken right: %v", err)
	}

	if left == right {
		t.Fatalf("expected distinct tokens, got %q", left)
	}
	if len(left) == 0 || len(right) == 0 {
		t.Fatalf("expected non-empty tokens")
	}
}

func TestHashTokenDeterministic(t *testing.T) {
	hashA := HashToken("abc123")
	hashB := HashToken("abc123")
	hashC := HashToken("different")

	if hashA != hashB {
		t.Fatalf("expected matching hashes, got %q and %q", hashA, hashB)
	}
	if hashA == hashC {
		t.Fatalf("expected different hashes for different input")
	}
	if !SecureCompare(hashA, hashB) {
		t.Fatalf("expected secure compare match")
	}
	if SecureCompare(hashA, hashC) {
		t.Fatalf("expected secure compare mismatch")
	}
}
