// Package scriptinfo turns a script into the numbers a creator plans with:
// how many words it holds, and how long those words take to say out loud.
//
// Runtime is reported as a range, not a single number. Delivery changes the
// answer by more than most people expect (a measured read and an energetic one
// differ by well over a minute on a ten-minute script), and the number is only
// ever used to judge whether a draft fits a slot. Three rates bracket the
// honest answer; one rate would hide the spread that matters.
package scriptinfo

import (
	"math"
	"strings"
	"time"
	"unicode"
)

// Speaking rates in words per minute. Typical is the middle of the range
// measured on YouTube voice-over; Slow and Fast are the ends a creator can
// actually land on.
const (
	SlowWPM    = 160
	TypicalWPM = 181
	FastWPM    = 201
)

// Estimate is one script's runtime at each of the three rates.
type Estimate struct {
	Slow    time.Duration
	Typical time.Duration
	Fast    time.Duration
}

// WordCount counts the words in text.
//
// A token counts when it holds at least one letter or digit. Transcripts and
// hand-written scripts both carry standalone punctuation (`--` scene breaks,
// a line that is only an em dash, stray `[MUSIC]`-style markers), and counting
// those as words would inflate the runtime of exactly the drafts that are
// already rough.
func WordCount(text string) int {
	n := 0
	for _, tok := range strings.Fields(text) {
		for _, r := range tok {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				n++
				break
			}
		}
	}
	return n
}

// Runtime estimates how long words take to say. Negative input reads as zero,
// so a caller that subtracted lengths from a half-read script cannot produce a
// negative duration.
func Runtime(words int) Estimate {
	if words < 0 {
		words = 0
	}
	return Estimate{
		Slow:    durationFor(words, SlowWPM),
		Typical: durationFor(words, TypicalWPM),
		Fast:    durationFor(words, FastWPM),
	}
}

// WordsFor is the inverse: how many words fit in d at each rate. It answers
// "how long should my script be for a 10-minute video", which is the question
// that sends people to this package in the first place. Negative d reads as
// zero.
func WordsFor(d time.Duration) (slow, typical, fast int) {
	minutes := d.Minutes()
	if minutes < 0 {
		minutes = 0
	}
	return int(math.Round(minutes * SlowWPM)),
		int(math.Round(minutes * TypicalWPM)),
		int(math.Round(minutes * FastWPM))
}

// durationFor is the one place the rate arithmetic lives, so a new rate cannot
// be added to Runtime without going through it.
func durationFor(words, wpm int) time.Duration {
	if wpm <= 0 {
		return 0
	}
	return time.Duration(float64(words) / float64(wpm) * float64(time.Minute))
}
