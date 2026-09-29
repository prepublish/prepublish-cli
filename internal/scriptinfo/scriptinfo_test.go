package scriptinfo

import (
	"testing"
	"time"
)

func TestWordCount(t *testing.T) {
	cases := []struct {
		name string
		text string
		want int
	}{
		{"empty", "", 0},
		{"whitespace only", "   \n\t\n ", 0},
		{"simple sentence", "This is a test.", 4},
		{"punctuation stays inside tokens", "Wait — really? Yes, truly.", 4},
		{"hyphenated and apostrophes are one word", "A well-known creator's hook.", 4},
		{"newlines split words", "one\ntwo\n\nthree", 3},
		{"standalone separators are not words", "one\n--\ntwo\n—\nthree", 3},
		{"scene markers count once for their words", "[MUSIC] intro", 2},
		{"digits count", "3 reasons 2 ship", 4},
		{"non-latin counts", "привет мир 世界", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := WordCount(tc.text); got != tc.want {
				t.Errorf("WordCount(%q) = %d, want %d", tc.text, got, tc.want)
			}
		})
	}
}

func TestRuntimeAtEachRate(t *testing.T) {
	// 1810 words is ten minutes at the typical rate by definition, which is the
	// check that the rate arithmetic and the unit conversion agree.
	got := Runtime(1810)
	if got.Typical != 10*time.Minute {
		t.Errorf("Runtime(1810).Typical = %v, want 10m", got.Typical)
	}
	// 1810/160 and 1810/201 minutes, to the nearest hundredth of a second.
	near := func(got, want time.Duration, rate int) {
		t.Helper()
		if diff := got - want; diff > time.Second || diff < -time.Second {
			t.Errorf("at %d wpm: got %v, want %v", rate, got, want)
		}
	}
	near(got.Slow, 11*time.Minute+18*time.Second+750*time.Millisecond, SlowWPM)
	near(got.Fast, 9*time.Minute+298*time.Millisecond, FastWPM)

	// A slower read takes longer, and the three are ordered by rate.
	if !(got.Slow > got.Typical && got.Typical > got.Fast) {
		t.Errorf("rates are not ordered: slow %v, typical %v, fast %v", got.Slow, got.Typical, got.Fast)
	}
}

func TestRuntimeClampsNegativeWordCounts(t *testing.T) {
	got := Runtime(-100)
	if got.Slow != 0 || got.Typical != 0 || got.Fast != 0 {
		t.Errorf("Runtime(-100) = %+v, want zeros: a duration cannot be negative", got)
	}
}

func TestWordsFor(t *testing.T) {
	slow, typical, fast := WordsFor(10 * time.Minute)
	if slow != 1600 || typical != 1810 || fast != 2010 {
		t.Errorf("WordsFor(10m) = (%d, %d, %d), want (1600, 1810, 2010)", slow, typical, fast)
	}
	// Faster delivery fits more words in the same slot.
	if !(fast > typical && typical > slow) {
		t.Errorf("counts are not ordered: %d, %d, %d", slow, typical, fast)
	}

	if s, ty, f := WordsFor(-time.Minute); s != 0 || ty != 0 || f != 0 {
		t.Errorf("WordsFor(-1m) = (%d, %d, %d), want zeros", s, ty, f)
	}
}

func TestRuntimeAndWordsForRoundTrip(t *testing.T) {
	for _, words := range []int{0, 1, 250, 1810, 5432, 100000} {
		slow, typical, fast := WordsFor(Runtime(words).Typical)
		// Rounding through a duration costs less than a word per rate.
		if abs(typical-words) > 1 {
			t.Errorf("round trip typical: %d words -> %d words", words, typical)
		}
		if slow > words || fast < words {
			t.Errorf("round trip ordering broken at %d words: slow %d, fast %d", words, slow, fast)
		}
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
