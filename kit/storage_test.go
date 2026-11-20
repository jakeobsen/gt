package kit

import (
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

type memStore map[string]string

func (m memStore) Get(k string) (string, bool) { v, ok := m[k]; return v, ok }
func (m memStore) Set(k, v string) error       { m[k] = v; return nil }
func (m memStore) Delete(k string) error       { delete(m, k); return nil }
func (m memStore) Keys() []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func TestFrecencyOrdersByUseAndAge(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	f := NewFrecency(memStore{}, "projects")
	at := func(d time.Duration) { f.now = func() time.Time { return now.Add(d) } }

	at(-30 * 24 * time.Hour) // long ago: used a lot
	for range 4 {
		f.Visit("old-favourite")
	}
	at(-time.Hour) // recently: used twice
	f.Visit("recent")
	f.Visit("recent")
	at(0)

	items := []string{"zeta", "old-favourite", "alpha", "recent"}
	SortByFrecency(f, items, func(s string) string { return s }, strings.Compare)
	if want := []string{"recent", "old-favourite", "alpha", "zeta"}; !slices.Equal(items, want) {
		t.Fatalf("order = %v, want %v (4 visits 30 days ago decay below 2 visits today; unvisited by name)", items, want)
	}

	if err := f.Reset("recent"); err != nil || f.Score("recent") != 0 {
		t.Fatalf("Reset: err=%v score=%v", err, f.Score("recent"))
	}
}
