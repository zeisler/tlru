// * tlru <https://github.com/jahnestacado/go-tlru>
// * Copyright (c) 2020 Ioannis Tzanellis
// * Licensed under the MIT License (MIT).

// Package tlru (Time aware Least Recently Used) cache
package tlru

import (
	"fmt"
	"sync"
	"time"
)

// TLRU cache public interface
type TLRU interface {
	// Get retrieves an entry from the cache by key
	// Get behaves differently depending on the EvictionPolicy used
	// * EvictionPolicy.LRA - (Least Recenty Accessed):
	//		- If the key entry exists then the entry is marked as the
	//		  most recently used entry
	//		- If the key entry exists then the entrys Counter is incremented and the
	//		  LastUpdatedAt property is updated
	//		- If an entry for the specified key doesn't exist then it returns nil
	//		- If an entry for the specified key exists but is expired it returns nil
	//		  and an EvictedEntry will be emitted	to the EvictionChannel(if present)
	//		  with EvictionReasonExpired
	// * EvictionPolicy.LRI - (Least Recenty Inserted):
	//		- If an entry for the specified key doesn't exist then it returns nil
	//		- If an entry for the specified key exists but is expired it returns nil
	//		  and an EvictedEntry will be emitted to the EvictionChannel(if present)
	//		  with EvictionReasonExpired
	Get(key string) *CacheEntry

	// Set inserts/updates an entry in the cache
	// Set behaves differently depending on the EvictionPolicy used
	// * EvictionPolicy.LRA - (Least Recenty Accessed):
	//		- If the key entry doesn't exist then it inserts it as the most
	//		  recently used entry
	//		- If the key entry already exists then it will replace the existing
	//		  entry with the new one as the most recently used entry.
	//		  An EvictedEntry will be emitted to the EvictionChannel(if present)
	//		  with EvictionReasonReplaced. Replace means that the entry will be
	//		  dropped and re-inserted with a new CreatedAt/LastUpdatedAt timestamp
	//		  and a resetted Counter
	//		- If the cache is full (Config.Size) then the least recently accessed
	//		  entry(the node before the tailNode) will be dropped and an
	//		  EvictedEntry will be emitted to the EvictionChannel(if present)
	//		  with EvictionReasonDropped
	// * EvictionPolicy.LRI - (Least Recenty Inserted):
	//		- If the key entry doesn't exist then it inserts it as the
	//		  most recently used entry
	//		- If the key entry already exists then it will update
	//		  the Value, Counter, LastUpdatedAt, CreatedAt properties of
	//		  the existing entry and mark it as the most recently used entry
	//		- If the cache is full (Config.Size) then
	//		  the least recently inserted entry(the node before the tailNode)
	//		  will be dropped and an EvictedEntry will be emitted to
	//		  the EvictionChannel(if present) with EvictionReasonDropped
	Set(entry Entry)

	// Delete removes the entry that corresponds to the provided key from cache
	// An EvictedEntry will be emitted to the EvictionChannel(if present)
	// with EvictionReasonDeleted
	Delete(key string)

	// Keys returns an unordered slice of all available keys in the cache
	// The order of keys is not guaranteed
	// It will also evict expired entries based on the TTL of the cache
	Keys() []string

	// Entries returns an unordered slice of all available entries in the cache
	// The order of entries is not guaranteed
	// It will also evict expired entries based on the TTL of the cache
	Entries() []CacheEntry

	// Clear removes all entries from the cache
	Clear()

	// GetState returns the internal State of the cache
	// This State can be put in persistent storage and rehydrated at a later point
	// via the SetState method
	GetState() State

	// SetState sets the internal State of the cache
	SetState(state State) error
}

// Config of tlru cache
type Config struct {
	// Max size of cache
	Size int
	// Time to live of cached entries
	TTL time.Duration
	// Channel to listen for evicted entries events
	EvictionChannel *chan EvictedEntry
	// Eviction policy of tlru. Default is LRA
	EvictionPolicy evictionPolicy
}

// Entry to be cached
type Entry struct {
	// The unique identifier of entry
	Key string `json:"key"`
	// The value to be cached
	Value interface{} `json:"value"`
	// Optional field. If provided TTL of entry will be checked against this field
	Timestamp *time.Time `json:"timestamp"`
}

// CacheEntry holds the cached value along with some additional information
type CacheEntry struct {
	// The cached value
	Value interface{} `json:"value"`
	// The number of times this entry has been inserted or accessed based
	// on the EvictionPolicy
	Counter int64 `json:"counter"`
	// The time that this entry was last inserted or accessed based
	// on the EvictionPolicy
	LastUpdatedAt time.Time `json:"last_updated_at"`
	// The time this entry was inserted to the cache
	CreatedAt time.Time `json:"created_at"`
}

// EvictedEntry is an entry that is removed from the cache due to
// an evictionReason
type EvictedEntry struct {
	// The unique identifier of entry
	Key string `json:"key"`
	// The cached value
	Value interface{} `json:"value"`
	// The number of times this entry has been inserted or accessed based on
	// the EvictionPolicy
	Counter int64 `json:"counter"`
	// The time that this entry was last inserted or accessed based on
	// the EvictionPolicy
	LastUpdatedAt time.Time `json:"last_updated_at"`
	// The time this entry was inserted to the cache
	CreatedAt time.Time `json:"created_at"`
	// The time this entry was evicted from the cache
	EvictedAt time.Time `json:"evicted_at"`
	// The reason this entry has been removed
	Reason evictionReason `json:"reason"`
}

// State is the internal representation of the cache.
// State can be retrieved/set via the GetState/SetState methods respectively
type State struct {
	Entries        []StateEntry   `json:"entries"`
	EvictionPolicy evictionPolicy `json:"eviction_policy"`
	ExtractedAt    time.Time      `json:"extracted_at"`
}

// StateEntry is a representation of a doublyLinkedNode without pointer references
type StateEntry struct {
	Key           string      `json:"key"`
	Value         interface{} `json:"value"`
	Counter       int64       `json:"counter"`
	LastUpdatedAt time.Time   `json:"last_updated_at"`
	CreatedAt     time.Time   `json:"created_at"`
}

const (
	// LRA - Least Recenty Accessed
	LRA evictionPolicy = iota
	// LRI - Least Recenty Inserted
	LRI
)

const (
	// EvictionReasonDropped occurs when cache is full
	EvictionReasonDropped evictionReason = iota
	// EvictionReasonExpired occurs when the TTL of an entry is expired
	EvictionReasonExpired
	// EvictionReasonReplaced occurs when the corresponding CacheEntry of a key is replaced.
	// Can happen only when the EvictionPolicy is LRA
	EvictionReasonReplaced
	// EvictionReasonDeleted occurs when the Delete method is called for a key
	EvictionReasonDeleted
)

type tlru struct {
	sync.RWMutex
	cache          map[string]*doublyLinkedNode
	config         Config
	headNode       *doublyLinkedNode
	tailNode       *doublyLinkedNode
	evictionPolicy evictionPolicy
}

// New returns a new instance of TLRU cache
func New(config Config) TLRU {
	headNode := &doublyLinkedNode{key: "head_node"}
	tailNode := &doublyLinkedNode{key: "tail_node"}
	headNode.next = tailNode
	tailNode.previous = headNode

	cache := &tlru{
		config: config,
		cache:  make(map[string]*doublyLinkedNode, 0),
	}

	cache.initializeDoublyLinkedList()
	cache.startTTLEvictionDaemon()

	return cache
}

func (c *tlru) Get(key string) *CacheEntry {
	defer c.Unlock()
	c.Lock()

	linkedNode, exists := c.cache[key]
	if !exists {
		return nil
	}

	if c.config.TTL < time.Since(linkedNode.lastUpdatedAt) {
		c.evictEntry(linkedNode, EvictionReasonExpired)
		return nil
	}

	if c.config.EvictionPolicy == LRA {
		c.setMRUNode(Entry{Key: key, Value: linkedNode.value})
	}

	cacheEntry := linkedNode.ToCacheEntry()

	return &cacheEntry
}

func (c *tlru) Set(entry Entry) {
	defer c.Unlock()
	c.Lock()

	linkedEntry, exists := c.cache[entry.Key]
	if exists && c.config.EvictionPolicy == LRA {
		c.evictEntry(linkedEntry, EvictionReasonReplaced)
	}

	if !exists && len(c.cache) == c.config.Size {
		c.evictEntry(c.tailNode.previous, EvictionReasonDropped)
	}

	c.setMRUNode(entry)
}

func (c *tlru) Delete(key string) {
	defer c.Unlock()
	c.Lock()

	linkedNode, exists := c.cache[key]
	if exists {
		c.evictEntry(linkedNode, EvictionReasonDeleted)
	}
}

func (c *tlru) Keys() []string {
	defer c.Unlock()
	c.Lock()
	c.evictExpiredEntries()

	keys := make([]string, 0, len(c.cache))
	for key := range c.cache {
		keys = append(keys, key)
	}

	return keys
}

func (c *tlru) Entries() []CacheEntry {
	defer c.Unlock()
	c.Lock()
	c.evictExpiredEntries()

	entries := make([]CacheEntry, 0, len(c.cache))
	for _, linkedNode := range c.cache {
		entries = append(entries, linkedNode.ToCacheEntry())
	}

	return entries
}

func (c *tlru) Clear() {
	defer c.Unlock()
	c.Lock()

	c.clear()
}

func (c *tlru) GetState() State {
	defer c.RUnlock()
	c.RLock()

	state := State{
		EvictionPolicy: c.config.EvictionPolicy,
		Entries:        make([]StateEntry, 0, len(c.cache)),
		ExtractedAt:    time.Now().UTC(),
	}

	nextNode := c.headNode.next
	for nextNode != nil && nextNode != c.tailNode {
		state.Entries = append(state.Entries, nextNode.ToStateEntry())
		nextNode = nextNode.next
	}

	return state
}

func (c *tlru) SetState(state State) error {
	defer c.Unlock()
	c.Lock()
	if state.EvictionPolicy != c.config.EvictionPolicy {
		return fmt.Errorf("tlru.SetState: Incompatible state EvictionPolicy %s", state.EvictionPolicy.String())
	}
	c.clear()

	previousNode := c.headNode
	cache := make(map[string]*doublyLinkedNode, 0)
	for _, StateEntry := range state.Entries {
		rehydratedNode := &doublyLinkedNode{
			key:           StateEntry.Key,
			value:         StateEntry.Value,
			counter:       StateEntry.Counter,
			lastUpdatedAt: StateEntry.LastUpdatedAt,
			createdAt:     StateEntry.CreatedAt,
		}
		previousNode.next = rehydratedNode
		rehydratedNode.previous = previousNode
		previousNode = rehydratedNode
		cache[rehydratedNode.key] = rehydratedNode
	}
	previousNode.next = c.tailNode
	c.tailNode.previous = previousNode
	c.cache = cache

	return nil
}

type doublyLinkedNode struct {
	key           string
	value         interface{}
	counter       int64
	lastUpdatedAt time.Time
	createdAt     time.Time
	previous      *doublyLinkedNode
	next          *doublyLinkedNode
}

func (d *doublyLinkedNode) ToCacheEntry() CacheEntry {
	return CacheEntry{
		Value:         d.value,
		Counter:       d.counter,
		LastUpdatedAt: d.lastUpdatedAt,
		CreatedAt:     d.createdAt,
	}
}
func (d *doublyLinkedNode) ToEvictedEntry(reason evictionReason) EvictedEntry {
	return EvictedEntry{
		Key:           d.key,
		Value:         d.value,
		Counter:       d.counter,
		LastUpdatedAt: d.lastUpdatedAt,
		CreatedAt:     d.createdAt,
		EvictedAt:     time.Now().UTC(),
		Reason:        reason,
	}
}

func (d *doublyLinkedNode) ToStateEntry() StateEntry {
	return StateEntry{
		Key:           d.key,
		Value:         d.value,
		Counter:       d.counter,
		LastUpdatedAt: d.lastUpdatedAt,
		CreatedAt:     d.createdAt,
	}
}

type evictionReason int

type evictionPolicy int

func (p evictionPolicy) String() string {
	return [...]string{0: "LRA", 1: "LRI"}[p]
}

func (c *tlru) clear() {
	if len(c.cache) > 0 {
		c.cache = make(map[string]*doublyLinkedNode, 0)
		c.initializeDoublyLinkedList()
	}
}

func (c *tlru) initializeDoublyLinkedList() {
	headNode := &doublyLinkedNode{key: "head_node"}
	tailNode := &doublyLinkedNode{key: "tail_node"}
	headNode.next = tailNode
	tailNode.previous = headNode
	c.headNode = headNode
	c.tailNode = tailNode
}

func (c *tlru) setMRUNode(entry Entry) {
	var counter int64
	if c.config.EvictionPolicy == LRI {
		counter++
	}

	lastUpdatedAt := time.Now().UTC()
	if entry.Timestamp != nil {
		lastUpdatedAt = *entry.Timestamp
	}
	linkedNode, exists := c.cache[entry.Key]
	if exists {
		if c.config.TTL >= time.Since(linkedNode.lastUpdatedAt) {
			linkedNode.counter++
		}
		linkedNode.lastUpdatedAt = lastUpdatedAt

		// Re-wire siblings of linkedNode
		linkedNode.next.previous = linkedNode.previous
		linkedNode.previous.next = linkedNode.next
	} else {
		linkedNode = &doublyLinkedNode{
			key:           entry.Key,
			value:         entry.Value,
			counter:       counter,
			lastUpdatedAt: lastUpdatedAt,
			previous:      c.headNode,
			next:          c.headNode.next,
			createdAt:     time.Now().UTC(),
		}

		c.cache[entry.Key] = linkedNode
	}

	// Re-wire headNode
	linkedNode.previous = c.headNode
	linkedNode.next = c.headNode.next
	c.headNode.next.previous = linkedNode
	c.headNode.next = linkedNode
}

func (c *tlru) evictEntry(evictedNode *doublyLinkedNode, reason evictionReason) {
	evictedNode.previous.next = evictedNode.next
	evictedNode.next.previous = evictedNode.previous
	delete(c.cache, evictedNode.key)

	if c.config.EvictionChannel != nil {
		*c.config.EvictionChannel <- evictedNode.ToEvictedEntry(reason)
	}
}

func (c *tlru) evictExpiredEntries() {
	previousNode := c.tailNode.previous
	for previousNode != nil && previousNode != c.headNode {
		if c.config.TTL < time.Since(previousNode.lastUpdatedAt) {
			c.evictEntry(previousNode, EvictionReasonExpired)
		}
		previousNode = previousNode.previous
	}
}

func (c *tlru) startTTLEvictionDaemon() {
	go func() {
		for {
			time.Sleep(c.config.TTL)
			c.Lock()
			c.evictExpiredEntries()
			c.Unlock()
		}
	}()
}
