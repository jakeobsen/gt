package scratch

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Two ~64-word lists give ~4k word pairs. Collisions are handled by the
// caller (it re-rolls, then appends -2, -3, …), so the lists only need to be
// big enough that names stay distinguishable at a glance.
var adjectives = []string{
	"amber", "ancient", "bold", "brave", "brisk", "calm", "chatty", "clever",
	"cosmic", "crimson", "curious", "dapper", "dusty", "eager", "electric", "fancy",
	"feral", "fluffy", "fuzzy", "gentle", "gilded", "glossy", "golden", "happy",
	"hidden", "humble", "icy", "idle", "jolly", "keen", "lazy", "lively",
	"lucid", "lucky", "mellow", "merry", "misty", "narrow", "neat", "nimble",
	"noisy", "odd", "polite", "quiet", "rapid", "rowdy", "rustic", "salty",
	"shiny", "silent", "sleepy", "smooth", "snappy", "solar", "spicy", "stormy",
	"sturdy", "sunny", "tidy", "tiny", "velvet", "witty", "wobbly", "zesty",
}

var nouns = []string{
	"anchor", "badger", "beacon", "bison", "bramble", "cactus", "canyon", "cedar",
	"cinder", "comet", "compass", "coral", "crane", "dolphin", "ember", "falcon",
	"fern", "ferret", "fjord", "forge", "gecko", "geyser", "glacier", "gopher",
	"harbor", "hazel", "heron", "ibex", "jasper", "kelp", "lantern", "lemur",
	"lichen", "lynx", "magpie", "marble", "meadow", "mesa", "narwhal", "nebula",
	"newt", "onyx", "otter", "owl", "panda", "pebble", "pelican", "pine",
	"quartz", "quokka", "raccoon", "reef", "ridge", "salmon", "sparrow", "spruce",
	"summit", "tapir", "thicket", "tundra", "vulture", "walrus", "willow", "zebra",
}

func pick(list []string) string { return list[rand.IntN(len(list))] }

func randomHex(bytes int) string {
	var b strings.Builder
	for range bytes {
		fmt.Fprintf(&b, "%02x", rand.IntN(256))
	}
	return b.String()
}

// slugify turns free text into a kebab-case, path-safe slug of at most 40
// characters ("" when nothing survives). Accents are folded away, so
// "ünïcode" becomes "unicode" rather than "u-ni-code".
func slugify(input string) string {
	var b strings.Builder
	dash := false
	for _, r := range norm.NFKD.String(input) {
		switch {
		case unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Mc, r) || unicode.Is(unicode.Me, r):
			// combining mark split off by NFKD: drop it
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(unicode.ToLower(r))
		default:
			dash = true
		}
	}
	slug := []rune(b.String())
	if len(slug) > 40 {
		slug = slug[:40]
	}
	return strings.TrimRight(string(slug), "-")
}

// generateName makes a random folder name in the given style, prefixed with
// the slug of label when there is one.
func generateName(style, label string, now time.Time) string {
	var random string
	switch style {
	case "hex":
		random = "scratch-" + randomHex(4)
	case "words-suffix":
		random = pick(adjectives) + "-" + pick(nouns) + "-" + randomHex(2)
	case "date-words":
		random = now.Format("2006-01-02") + "-" + pick(adjectives) + "-" + pick(nouns)
	default:
		random = pick(adjectives) + "-" + pick(nouns)
	}
	if slug := slugify(label); slug != "" {
		return slug + "-" + random
	}
	return random
}
