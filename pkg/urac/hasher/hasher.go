package hasher

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"

	"golang.org/x/crypto/bcrypt"
)

// Config holds hasher configuration
type Config struct {
	// HashIterations for bcrypt cost (default: 12)
	HashIterations int
	// OptionalAlgorithm for pre-hashing (e.g., "sha256", "sha512")
	OptionalAlgorithm string
}

// DefaultConfig returns default hasher configuration
func DefaultConfig() Config {
	return Config{
		HashIterations:    12,
		OptionalAlgorithm: "",
	}
}

// Hasher provides password hashing functionality
type Hasher struct {
	config Config
}

// New creates a new Hasher with the given config
func New(cfg Config) *Hasher {
	if cfg.HashIterations < 4 {
		cfg.HashIterations = 12
	}
	if cfg.HashIterations > 31 {
		cfg.HashIterations = 31
	}
	return &Hasher{config: cfg}
}

// Hash creates a password hash
func (h *Hasher) Hash(password string) (string, error) {
	// Apply optional algorithm first
	pwd := h.applyOptionalAlgorithm(password)

	// Generate bcrypt hash
	hash, err := bcrypt.GenerateFromPassword([]byte(pwd), h.config.HashIterations)
	if err != nil {
		return "", fmt.Errorf("failed to hash password: %w", err)
	}

	return string(hash), nil
}

// Compare compares a password with a hash
func (h *Hasher) Compare(password, hashedPassword string) error {
	// Apply optional algorithm first
	pwd := h.applyOptionalAlgorithm(password)

	// Compare with bcrypt
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(pwd))
	if err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return ErrPasswordMismatch
		}
		return fmt.Errorf("failed to compare password: %w", err)
	}

	return nil
}

// applyOptionalAlgorithm applies optional pre-hashing algorithm
func (h *Hasher) applyOptionalAlgorithm(password string) string {
	if h.config.OptionalAlgorithm == "" {
		return password
	}

	var hasher hash.Hash
	switch h.config.OptionalAlgorithm {
	case "sha256":
		hasher = sha256.New()
	case "sha512":
		hasher = sha512.New()
	default:
		// Unknown algorithm, return as-is
		return password
	}

	hasher.Write([]byte(password))
	result := hex.EncodeToString(hasher.Sum(nil))

	// Truncate to 72 bytes (bcrypt's max input length)
	if len(result) > 72 {
		result = result[:72]
	}

	return result
}

// ErrPasswordMismatch is returned when password doesn't match
var ErrPasswordMismatch = errors.New("password mismatch")

// ComparePassword compares a password with a hash using the given config
func ComparePassword(password, hashedPassword string, cfg Config) error {
	h := New(cfg)
	return h.Compare(password, hashedPassword)
}

// HashPassword creates a password hash using the given config
func HashPassword(password string, cfg Config) (string, error) {
	h := New(cfg)
	return h.Hash(password)
}

// QuickCompare compares password with hash using default config
func QuickCompare(password, hashedPassword string) error {
	return ComparePassword(password, hashedPassword, DefaultConfig())
}

// QuickHash creates a password hash using default config
func QuickHash(password string) (string, error) {
	return HashPassword(password, DefaultConfig())
}
