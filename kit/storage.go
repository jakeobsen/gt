package kit

import (
	"fmt"
	"math"
	"slices"
	"time"
)

// Storage is a plugin's own small key-value store that persists between
// runs, like Raycast's LocalStorage. Each plugin gets its own; values are
// plain strings, so store JSON for anything structured.
type Storage interface {
	Get(key string) (string, bool)
	Set(key, value string) error
	Delete(key string) error
	// Keys lists every key, sorted.
	Keys() []string
}

// frecencyHalfLife is how long it takes a visit to count half as much.
const frecencyHalfLife = 10 * 24 * time.Hour

// Frecency ranks items by how often and how recently they were used, like
// Raycast's useFrecencySorting. Each visit adds a point; points halve every
// ten days.
type Frecency struct {
	store  Storage
	prefix string
	now    func() time.Time
}

// NewFrecency keeps rankings named name in store.
func NewFrecency(store Storage, name string) *Frecency {
	return &Frecency{store: store, prefix: "frecency/" + name + "/", now: time.Now}
}

// Score is id's current ranking: 0 when it was never visited.
func (f *Frecency) Score(id string) float64 {
	v, ok := f.store.Get(f.prefix + id)
	if !ok {
		return 0
	}
	var score float64
	var last int64
	if _, err := fmt.Sscanf(v, "%g %d", &score, &last); err != nil {
		return 0
	}
	age := f.now().Sub(time.UnixMilli(last))
	return score * math.Exp2(-float64(age)/float64(frecencyHalfLife))
}

// Visit records a use of id.
func (f *Frecency) Visit(id string) error {
	score := f.Score(id) + 1
	return f.store.Set(f.prefix+id, fmt.Sprintf("%g %d", score, f.now().UnixMilli()))
}

// Reset forgets every use of id.
func (f *Frecency) Reset(id string) error { return f.store.Delete(f.prefix + id) }

// SortByFrecency sorts items with the highest-ranked first. Items never
// visited come after, ordered by unvisited (a cmp-style function).
func SortByFrecency[T any](f *Frecency, items []T, id func(T) string, unvisited func(a, b T) int) {
	scores := make(map[string]float64, len(items))
	for _, it := range items {
		scores[id(it)] = f.Score(id(it))
	}
	slices.SortStableFunc(items, func(a, b T) int {
		sa, sb := scores[id(a)], scores[id(b)]
		switch {
		case sa > sb:
			return -1
		case sa < sb:
			return 1
		case sa == 0:
			return unvisited(a, b)
		}
		return 0
	})
}
