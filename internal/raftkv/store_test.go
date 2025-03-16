package raftkv

import (
	"testing"
)

// Test store creation
func TestNewStore(t *testing.T) {
	store := NewStore()
	if store == nil {
		t.Fatal("NewStore() returned nil")
	}
}

// Test Set and Get
func TestSetAndGet(t *testing.T) {
	store := NewStore()
	key := "foo"
	value := []byte("bar")

	store.Set(key, value)

	retrieved, err := store.Get(key)
	if err != nil {
		t.Fatalf("Expected value, got error: %v", err)
	}
	if string(retrieved) != string(value) {
		t.Errorf("Expected %s, got %s", value, retrieved)
	}
}

// Test Get with non-existent key
func TestGetNonExistentKey(t *testing.T) {
	store := NewStore()
	_, err := store.Get("missing")

	if err == nil {
		t.Error("Expected error for missing key, got nil")
	}
}

// Test Delete
func TestDelete(t *testing.T) {
	store := NewStore()
	key := "temp"
	value := []byte("data")

	store.Set(key, value)
	store.Delete(key)

	_, err := store.Get(key)
	if err == nil {
		t.Error("Expected error after deleting key, but got nil")
	}
}
