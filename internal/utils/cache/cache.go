// This implementation is based on and modified from https://github.com/fanjindong/go-cache
package cache

import (
	"fmt"
	"unsafe"

	"github.com/cespare/xxhash/v2"
)

// hashKey computes a 64-bit hash for a cache key. The hot paths in this
// codebase key caches by int / int64 / string (channel IDs, group IDs, API
// key IDs, setting names), so those are handled without the reflection and
// allocation cost of fmt.Sprintf. Other comparable types fall back to the
// generic formatting path.
func hashKey[K comparable](key K) uint64 {
	switch k := any(key).(type) {
	case string:
		return xxhash.Sum64String(k)
	case int:
		return hashUint64(uint64(k))
	case int64:
		return hashUint64(uint64(k))
	case int32:
		return hashUint64(uint64(uint32(k)))
	case uint:
		return hashUint64(uint64(k))
	case uint64:
		return hashUint64(k)
	case uint32:
		return hashUint64(uint64(k))
	default:
		return xxhash.Sum64String(fmt.Sprintf("%v", key))
	}
}

// hashUint64 hashes an 8-byte integer without allocating an intermediate
// string. The bytes are read directly from the value via an unsafe slice
// header; this never escapes hashKey and is purely read-only.
func hashUint64(v uint64) uint64 {
	b := (*[8]byte)(unsafe.Pointer(&v))
	return xxhash.Sum64(b[:])
}

type Cache[K comparable, V any] interface {
	Set(k K, v V)
	Get(k K) (V, bool)
	GetAll() map[K]V
	Del(keys ...K) int
	Exists(keys ...K) bool
	Len() int
	Clear()
}

func New[K comparable, V any](shards int) Cache[K, V] {
	if shards <= 0 {
		shards = 1024
	}

	c := &cache[K, V]{
		shards:    make([]*shard[K, V], shards),
		shardMask: uint64(shards - 1),
	}
	for i := 0; i < shards; i++ {
		c.shards[i] = &shard[K, V]{hashmap: map[K]V{}}
	}

	return c
}

type cache[K comparable, V any] struct {
	shards    []*shard[K, V]
	shardMask uint64
}

func (c *cache[K, V]) Set(k K, v V) {
	shard := c.getShard(hashKey(k))
	shard.set(k, v)
}

func (c *cache[K, V]) Get(k K) (V, bool) {
	shard := c.getShard(hashKey(k))
	return shard.get(k)
}

func (c *cache[K, V]) GetAll() map[K]V {
	result := make(map[K]V)
	for _, shard := range c.shards {
		shardData := shard.getAll()
		for k, v := range shardData {
			result[k] = v
		}
	}
	return result
}

func (c *cache[K, V]) Del(ks ...K) int {
	var count int
	for _, k := range ks {
		shard := c.getShard(hashKey(k))
		count += shard.del(k)
	}
	return count
}

func (c *cache[K, V]) Exists(ks ...K) bool {
	for _, k := range ks {
		if _, found := c.Get(k); !found {
			return false
		}
	}
	return true
}

func (c *cache[K, V]) Len() int {
	var count int
	for _, shard := range c.shards {
		count += shard.len()
	}
	return count
}

func (c *cache[K, V]) getShard(hashedKey uint64) (shard *shard[K, V]) {
	return c.shards[hashedKey&c.shardMask]
}

func (c *cache[K, V]) Clear() {
	for _, s := range c.shards {
		s.clear()
	}
}
