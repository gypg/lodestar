package cache

import (
	"strconv"
	"testing"
)

func TestCacheIntKeys(t *testing.T) {
	c := New[int, string](16)
	for i := 0; i < 1000; i++ {
		c.Set(i, strconv.Itoa(i))
	}
	if c.Len() != 1000 {
		t.Fatalf("expected 1000 entries, got %d", c.Len())
	}
	for i := 0; i < 1000; i++ {
		v, ok := c.Get(i)
		if !ok || v != strconv.Itoa(i) {
			t.Fatalf("key %d: got (%q, %v), want (%q, true)", i, v, ok, strconv.Itoa(i))
		}
	}
	if n := c.Del(0, 1, 2); n != 3 {
		t.Fatalf("expected to delete 3, got %d", n)
	}
	if _, ok := c.Get(0); ok {
		t.Fatal("key 0 should have been deleted")
	}
	if !c.Exists(3) {
		t.Fatal("key 3 should still exist")
	}
}

func TestCacheStringKeys(t *testing.T) {
	c := New[string, int](16)
	keys := []string{"retry.max", "circuit.threshold", "semantic.enabled", ""}
	for i, k := range keys {
		c.Set(k, i)
	}
	for i, k := range keys {
		v, ok := c.Get(k)
		if !ok || v != i {
			t.Fatalf("key %q: got (%d, %v), want (%d, true)", k, v, ok, i)
		}
	}
}

func TestCacheInt64Keys(t *testing.T) {
	c := New[int64, string](16)
	c.Set(int64(1<<40), "big")
	v, ok := c.Get(int64(1 << 40))
	if !ok || v != "big" {
		t.Fatalf("got (%q, %v), want (\"big\", true)", v, ok)
	}
}

// distinct integer keys must land deterministically; this guards the
// unsafe byte-hash path against collisions for small ints sharing a shard.
func TestCacheNoCrossKeyOverwrite(t *testing.T) {
	c := New[int, int](4)
	for i := 0; i < 10000; i++ {
		c.Set(i, i*7)
	}
	for i := 0; i < 10000; i++ {
		v, ok := c.Get(i)
		if !ok || v != i*7 {
			t.Fatalf("key %d corrupted: got (%d, %v)", i, v, ok)
		}
	}
}
