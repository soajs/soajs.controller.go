package hasher

import (
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.HashIterations != 12 {
		t.Errorf("Expected HashIterations 12, got %d", cfg.HashIterations)
	}

	if cfg.OptionalAlgorithm != "" {
		t.Errorf("Expected empty OptionalAlgorithm, got %s", cfg.OptionalAlgorithm)
	}
}

func TestNew(t *testing.T) {
	tests := []struct {
		name       string
		iterations int
		wantCost   int
	}{
		{"default iterations", 12, 12},
		{"low iterations clamped", 2, 12},
		{"high iterations clamped", 35, 31},
		{"valid iterations", 10, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := New(Config{HashIterations: tt.iterations})
			if h.config.HashIterations != tt.wantCost {
				t.Errorf("Expected cost %d, got %d", tt.wantCost, h.config.HashIterations)
			}
		})
	}
}

func TestHasher_HashAndCompare(t *testing.T) {
	h := New(Config{HashIterations: 4}) // Low cost for faster tests

	password := "testpassword123"

	hash, err := h.Hash(password)
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}

	if hash == "" {
		t.Error("Hash() returned empty string")
	}

	// Verify the hash
	err = h.Compare(password, hash)
	if err != nil {
		t.Errorf("Compare() error = %v, expected nil", err)
	}

	// Wrong password should fail
	err = h.Compare("wrongpassword", hash)
	if err != ErrPasswordMismatch {
		t.Errorf("Compare() with wrong password: expected ErrPasswordMismatch, got %v", err)
	}
}

func TestHasher_WithSHA256(t *testing.T) {
	h := New(Config{
		HashIterations:    4,
		OptionalAlgorithm: "sha256",
	})

	password := "testpassword123"

	hash, err := h.Hash(password)
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}

	// Verify the hash
	err = h.Compare(password, hash)
	if err != nil {
		t.Errorf("Compare() error = %v, expected nil", err)
	}

	// Wrong password should fail
	err = h.Compare("wrongpassword", hash)
	if err != ErrPasswordMismatch {
		t.Errorf("Compare() with wrong password: expected ErrPasswordMismatch, got %v", err)
	}
}

func TestHasher_WithSHA512(t *testing.T) {
	h := New(Config{
		HashIterations:    4,
		OptionalAlgorithm: "sha512",
	})

	password := "testpassword123"

	hash, err := h.Hash(password)
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}

	// Verify the hash
	err = h.Compare(password, hash)
	if err != nil {
		t.Errorf("Compare() error = %v, expected nil", err)
	}
}

func TestHasher_UnknownAlgorithm(t *testing.T) {
	h := New(Config{
		HashIterations:    4,
		OptionalAlgorithm: "unknown",
	})

	password := "testpassword123"

	hash, err := h.Hash(password)
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}

	// Should work without pre-hashing
	err = h.Compare(password, hash)
	if err != nil {
		t.Errorf("Compare() error = %v, expected nil", err)
	}
}

func TestQuickHash(t *testing.T) {
	password := "testpassword"

	hash, err := QuickHash(password)
	if err != nil {
		t.Fatalf("QuickHash() error = %v", err)
	}

	if hash == "" {
		t.Error("QuickHash() returned empty string")
	}
}

func TestQuickCompare(t *testing.T) {
	password := "testpassword"

	hash, _ := QuickHash(password)

	err := QuickCompare(password, hash)
	if err != nil {
		t.Errorf("QuickCompare() error = %v", err)
	}

	err = QuickCompare("wrong", hash)
	if err != ErrPasswordMismatch {
		t.Errorf("QuickCompare() wrong password: expected ErrPasswordMismatch, got %v", err)
	}
}

func TestComparePassword(t *testing.T) {
	cfg := Config{HashIterations: 4}
	password := "testpassword"

	hash, _ := HashPassword(password, cfg)

	err := ComparePassword(password, hash, cfg)
	if err != nil {
		t.Errorf("ComparePassword() error = %v", err)
	}
}

func TestHashPassword(t *testing.T) {
	cfg := Config{HashIterations: 4}
	password := "testpassword"

	hash1, err := HashPassword(password, cfg)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	hash2, err := HashPassword(password, cfg)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	// Hashes should be different (due to random salt)
	if hash1 == hash2 {
		t.Error("HashPassword() should produce different hashes for same password")
	}
}
