package registry

import (
	"sync"
	"testing"
	"time"
)

func TestNewCache(t *testing.T) {
	ttl := 100 * time.Millisecond
	cache := NewCache(ttl)

	if cache == nil {
		t.Fatal("NewCache returned nil")
	}
	if cache.items == nil {
		t.Error("Cache items map is nil")
	}
	if cache.ttl != ttl {
		t.Errorf("Cache TTL = %v, want %v", cache.ttl, ttl)
	}
}

func TestCache_SetAndGet(t *testing.T) {
	cache := NewCache(time.Hour)

	// Set value
	cache.Set("key1", "value1")

	// Get value
	val, ok := cache.Get("key1")
	if !ok {
		t.Error("Expected to find key1")
	}
	if val != "value1" {
		t.Errorf("Got %v, want value1", val)
	}

	// Get non-existent key
	_, ok = cache.Get("nonexistent")
	if ok {
		t.Error("Should not find nonexistent key")
	}
}

func TestCache_SetWithTTL(t *testing.T) {
	cache := NewCache(time.Hour)

	// Set with short TTL
	cache.SetWithTTL("shortlived", "data", 50*time.Millisecond)

	// Should exist immediately
	val, ok := cache.Get("shortlived")
	if !ok || val != "data" {
		t.Error("Expected to find shortlived key immediately")
	}

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// Should be expired
	_, ok = cache.Get("shortlived")
	if ok {
		t.Error("Expected shortlived key to be expired")
	}
}

func TestCache_Delete(t *testing.T) {
	cache := NewCache(time.Hour)

	cache.Set("key1", "value1")
	cache.Set("key2", "value2")

	// Delete key1
	cache.Delete("key1")

	// key1 should be gone
	_, ok := cache.Get("key1")
	if ok {
		t.Error("Expected key1 to be deleted")
	}

	// key2 should still exist
	val, ok := cache.Get("key2")
	if !ok || val != "value2" {
		t.Error("Expected key2 to still exist")
	}

	// Delete non-existent key (should not panic)
	cache.Delete("nonexistent")
}

func TestCache_Clear(t *testing.T) {
	cache := NewCache(time.Hour)

	cache.Set("key1", "value1")
	cache.Set("key2", "value2")
	cache.Set("key3", "value3")

	// Clear all
	cache.Clear()

	// All keys should be gone
	if _, ok := cache.Get("key1"); ok {
		t.Error("Expected key1 to be cleared")
	}
	if _, ok := cache.Get("key2"); ok {
		t.Error("Expected key2 to be cleared")
	}
	if _, ok := cache.Get("key3"); ok {
		t.Error("Expected key3 to be cleared")
	}
}

func TestCache_Expiration(t *testing.T) {
	cache := NewCache(50 * time.Millisecond)

	cache.Set("expires", "soon")

	// Should exist immediately
	val, ok := cache.Get("expires")
	if !ok || val != "soon" {
		t.Error("Expected to find key immediately")
	}

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// Should be expired (Get returns false for expired items)
	_, ok = cache.Get("expires")
	if ok {
		t.Error("Expected key to be expired")
	}
}

func TestCache_OverwriteValue(t *testing.T) {
	cache := NewCache(time.Hour)

	cache.Set("key", "value1")
	cache.Set("key", "value2")

	val, ok := cache.Get("key")
	if !ok {
		t.Error("Expected to find key")
	}
	if val != "value2" {
		t.Errorf("Got %v, want value2", val)
	}
}

func TestCache_DifferentTypes(t *testing.T) {
	cache := NewCache(time.Hour)

	// Store different types
	cache.Set("string", "hello")
	cache.Set("int", 42)
	cache.Set("slice", []string{"a", "b", "c"})
	cache.Set("map", map[string]int{"x": 1, "y": 2})

	// Retrieve and verify
	if val, ok := cache.Get("string"); !ok || val != "hello" {
		t.Error("String type failed")
	}
	if val, ok := cache.Get("int"); !ok || val != 42 {
		t.Error("Int type failed")
	}
	if val, ok := cache.Get("slice"); !ok {
		t.Error("Slice type failed")
	} else if slice, ok := val.([]string); !ok || len(slice) != 3 {
		t.Error("Slice type assertion failed")
	}
	if val, ok := cache.Get("map"); !ok {
		t.Error("Map type failed")
	} else if m, ok := val.(map[string]int); !ok || m["x"] != 1 {
		t.Error("Map type assertion failed")
	}
}

func TestCache_Concurrent(t *testing.T) {
	cache := NewCache(time.Hour)
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			cache.Set("concurrent", n)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cache.Get("concurrent")
		}()
	}

	// Concurrent deletes
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cache.Delete("concurrent")
		}()
	}

	wg.Wait()
	// If we get here without deadlock or panic, test passes
}

func TestCache_NilValue(t *testing.T) {
	cache := NewCache(time.Hour)

	cache.Set("nilval", nil)

	val, ok := cache.Get("nilval")
	if !ok {
		t.Error("Expected to find key with nil value")
	}
	if val != nil {
		t.Errorf("Expected nil, got %v", val)
	}
}
