// Copyright 2017 Google Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package searchset generates hashes for all substrings of a text. Potential
// matches between two SearchSet objects can then be determined quickly.
// Generating the hashes can be expensive, so it's best to perform it once. If
// the text is part of a known corpus, then the SearchSet can be serialized and
// kept in an archive.
//
// Matching occurs by "mapping" ranges from the source text into the target
// text but still retaining the source order:
//
//   SOURCE: |-----------------------------|
//
//   TARGET: |*****************************************|
//
//   MAP SOURCE SECTIONS ONTO TARGET IN SOURCE ORDER:
//
//     S:  |-[--]-----[---]------[----]------|
//            /         |           \
//         |---|   |---------|   |-------------|
//     T: |*****************************************|
//
// Note that a single source range may match many different ranges in the
// target. The matching algorithm untangles these so that all matched ranges
// are in order with respect to the source ranges. This is especially important
// since the source text may occur more than once in the target text. The
// algorithm finds each potential occurrence of S in T and returns all as
// potential matched ranges.
package searchset

import (
	"encoding/gob"
	"fmt"
	"io"
	"sort"

	"github.com/google/licenseclassifier/stringclassifier/searchset/tokenizer"
)

// DefaultGranularity is the minimum size (in words) of the hash chunks.
const DefaultGranularity = 3

// SearchSet is a set of substrings that have hashes associated with them,
// making it fast to search for potential matches.
type SearchSet struct {
	// Tokens is a tokenized list of the original input string.
	Tokens tokenizer.Tokens
	// Hashes is a map of checksums to a range of tokens.
	Hashes tokenizer.Hash
	// Checksums is a list of checksums ordered from longest range to
	// shortest.
	Checksums []uint32
	// ChecksumRanges are the token ranges for the above checksums.
	ChecksumRanges tokenizer.TokenRanges

	nodes []*node
}

// node consists of a range of tokens along with the checksum for those tokens.
type node struct {
	checksum uint32
	tokens   *tokenizer.TokenRange
}

func (n *node) String() string {
	return fmt.Sprintf("[%d:%d]", n.tokens.Start, n.tokens.End)
}

// New creates a new SearchSet object. It generates a hash for each substring of "s".
func New(s string, granularity int) *SearchSet {
	toks := tokenizer.Tokenize(s)

	// Start generating hash values for all substrings within the text.
	h := make(tokenizer.Hash)
	checksums, tokenRanges := toks.GenerateHashes(h, func(a, b int) int {
		if a < b {
			return a
		}
		return b
	}(len(toks), granularity))
	sset := &SearchSet{
		Tokens:         toks,
		Hashes:         h,
		Checksums:      checksums,
		ChecksumRanges: tokenRanges,
	}
	sset.GenerateNodeList()
	return sset
}

// GenerateNodeList creates a node list out of the search set.
func (s *SearchSet) GenerateNodeList() {
	if len(s.Tokens) == 0 {
		return
	}

	for i := 0; i < len(s.Checksums); i++ {
		s.nodes = append(s.nodes, &node{
			checksum: s.Checksums[i],
			tokens:   s.ChecksumRanges[i],
		})
	}
}

// Serialize emits the SearchSet out so that it can be recreated at a later
// time.
func (s *SearchSet) Serialize(w io.Writer) error {
	return gob.NewEncoder(w).Encode(s)
}

// Deserialize reads a file with a serialized SearchSet in it and reconstructs it.
func Deserialize(r io.Reader, s *SearchSet) error {
	if err := gob.NewDecoder(r).Decode(&s); err != nil {
		return err
	}
	s.GenerateNodeList()
	return nil
}

// MatchRange is the range within the source text that is a match to the range
// in the target text.
type MatchRange struct {
	// Offsets into the source tokens.
	SrcStart, SrcEnd int
	// Offsets into the target tokens.
	TargetStart, TargetEnd int
}

// in returns true if the start and end are enclosed in the match range.
func (m *MatchRange) in(start, end int) bool {
	return start >= m.TargetStart && end <= m.TargetEnd
}

// MatchRanges is a list of "MatchRange"s. The ranges are monotonically
// increasing in value and indicate a single potential occurrence of the source
// text in the target text.
type MatchRanges []*MatchRange

func (m MatchRanges) Len() int      { return len(m) }
func (m MatchRanges) Swap(i, j int) { m[i], m[j] = m[j], m[i] }
func (m MatchRanges) Less(i, j int) bool {
	if m[i].TargetStart < m[j].TargetStart {
		return true
	}
	return m[i].TargetStart == m[j].TargetStart && m[i].SrcStart < m[j].SrcStart
}

// TargetRange is the start and stop token offsets into the target text.
func (m MatchRanges) TargetRange(target *SearchSet) (start, end int) {
	start = target.Tokens[m[0].TargetStart].Offset
	end = target.Tokens[m[len(m)-1].TargetEnd-1].Offset + len(target.Tokens[m[len(m)-1].TargetEnd-1].Text)
	return start, end
}

// in returns true if the start and end are enclosed in one of the match ranges.
func (m MatchRanges) in(start, end int) bool {
	for _, val := range m {
		if val.in(start, end) {
			return true
		}
	}
	return false
}

// Size is the number of source tokens that were matched.
func (m MatchRanges) Size() int {
	sum := 0
	for _, mr := range m {
		sum += mr.SrcEnd - mr.SrcStart
	}
	return sum
}

// FindPotentialMatches returns the ranges in the target (unknown) text that
// are best potential matches to the source (known) text.
func FindPotentialMatches(src, target *SearchSet) []MatchRanges {
	matchedRanges := getMatchedRanges(src, target)
	if len(matchedRanges) == 0 {
		return nil
	}

	// Cleanup the matching ranges so that we get the longest contiguous ranges.
	for i := 0; i < len(matchedRanges); i++ {
		matchedRanges[i] = coalesceMatchRanges(matchedRanges[i])
	}
	return matchedRanges
}

// getMatchedRanges finds the ranges in the target text that match the source
// text. There can be multiple occurrences of the source text within the target
// text. Each separate occurrence is an entry in the returned slice.
func getMatchedRanges(src, target *SearchSet) []MatchRanges {
	matched := targetMatchedRanges(src, target)
	if len(matched) == 0 {
		return nil
	}
	sort.Sort(matched)
	matched = untangleSourceRanges(matched)
	matchedRanges := splitRanges(matched)
	return mergeConsecutiveRanges(matchedRanges)
}

// targetMatchedRanges goes through the source and finds all matches in the target.
func targetMatchedRanges(src, target *SearchSet) MatchRanges {
	if src.nodes == nil {
		return nil
	}

	var matched MatchRanges
	for _, srcNode := range src.nodes {
		tr, ok := target.Hashes[srcNode.checksum]
		if !ok {
			// There isn't a match in the target.
			continue
		}

		// Go over the set of target ranges that are potential matches.
		for _, tv := range tr {
			if matched.in(tv.Start, tv.End) {
				// Matched within a larger range. Ignore.
				continue
			}

			// The matched sections from the source (S) should be
			// in the same order as in the target (T). Thus if
			// chunks A and C from S match in T at positions X and
			// Z, then chunk B, which is between A and C in S,
			// should be in a position between X and Z.
			for _, sv := range src.Hashes[srcNode.checksum] {
				matched = append(matched, &MatchRange{
					SrcStart:    sv.Start,
					SrcEnd:      sv.End,
					TargetStart: tv.Start,
					TargetEnd:   tv.End,
				})
			}
		}
	}
	return matched
}

// untangleSourceRanges goes through the ranges and removes any whose source
// ranges are "out of order". A source range is "out of order" if the source
// range is out of sequence with the source ranges before and after it. This
// happens when more than one source range maps to the same target range.
// E.g.:
//
//     SrcStart: 20, SrcEnd: 30, TargetStart: 127, TargetEnd: 137
//  1: SrcStart: 12, SrcEnd: 17, TargetStart: 138, TargetEnd: 143
//  2: SrcStart: 32, SrcEnd: 37, TargetStart: 138, TargetEnd: 143
//     SrcStart: 38, SrcEnd: 40, TargetStart: 144, TargetEnd: 146
//
// Here (1) is out of order, because the source range [12, 17) is out of
// sequence with the surrounding source sequences, but [32, 37) is.
func untangleSourceRanges(matched MatchRanges) MatchRanges {
	mr := MatchRanges{matched[0]}
NEXT:
	for i := 1; i < len(matched); i++ {
		if mr[len(mr)-1].TargetStart == matched[i].TargetStart && mr[len(mr)-1].TargetEnd == matched[i].TargetEnd {
			// The matched range has already been added.
			continue
		}

		if i+1 < len(matched) && equalTargetRange(matched[i], matched[i+1]) {
			// A sequence of ranges match the same target range.
			// Find the first one that has a source range greater
			// than the currently matched range. Omit all others.
			if matched[i].SrcStart > mr[len(mr)-1].SrcStart {
				mr = append(mr, matched[i])
				continue
			}

			for j := i + 1; j < len(matched) && equalTargetRange(matched[i], matched[j]); j++ {
				// Check subsequent ranges to see if we can
				// find one that matches in the correct order.
				if matched[j].SrcStart > mr[len(mr)-1].SrcStart {
					mr = append(mr, matched[j])
					i = j
					continue NEXT
				}
			}
		}

		mr = append(mr, matched[i])
	}
	return mr
}

// equalTargetRange returns true if the two MatchRange's cover the same target range.
func equalTargetRange(this, that *MatchRange) bool {
	return this.TargetStart == that.TargetStart && this.TargetEnd == that.TargetEnd
}

// splitRanges splits the matched ranges so that a single match range has a
// monotonically increasing source range (indicating a single, potential
// instance of the source in the target).
func splitRanges(matched MatchRanges) []MatchRanges {
	var matchedRanges []MatchRanges
	mr := MatchRanges{matched[0]}
	for i := 1; i < len(matched); i++ {
		if mr[len(mr)-1].SrcStart > matched[i].SrcStart {
			matchedRanges = append(matchedRanges, mr)
			mr = MatchRanges{matched[i]}
		} else {
			mr = append(mr, matched[i])
		}
	}
	matchedRanges = append(matchedRanges, mr)
	return matchedRanges
}

// mergeConsecutiveRanges goes through the matched ranges and merges
// consecutive ranges. Two ranges are consecutive if the end of the previous
// matched range and beginning of the next matched range overlap. "matched"
// should have 1 or more MatchRanges, each with one or more MatchRange objects.
func mergeConsecutiveRanges(matched []MatchRanges) []MatchRanges {
	mr := []MatchRanges{matched[0]}

	// Convenience functions.
	prevMatchedRange := func() MatchRanges {
		return mr[len(mr)-1]
	}
	prevMatchedRangeLastElem := func() *MatchRange {
		return prevMatchedRange()[len(prevMatchedRange())-1]
	}

	// This algorithm compares the start of each MatchRanges object to the
	// end of the previous MatchRanges object. If they overlap, then it
	// tries to combine them. Note that a 0 offset into a MatchRanges
	// object (e.g., matched[i][0]) is its first MatchRange, which
	// indicates the start of the whole matched range.
NEXT:
	for i := 1; i < len(matched); i++ {
		if prevMatchedRangeLastElem().TargetEnd > matched[i][0].TargetStart {
			// Consecutive matched ranges overlap. Merge them.
			if prevMatchedRangeLastElem().TargetStart < matched[i][0].TargetStart {
				// The last element of the previous matched
				// range overlaps with the first element of the
				// current matched range. Concatenate them.
				if prevMatchedRangeLastElem().TargetEnd < matched[i][0].TargetEnd {
					prevMatchedRangeLastElem().SrcEnd += matched[i][0].TargetEnd - prevMatchedRangeLastElem().TargetEnd
					prevMatchedRangeLastElem().TargetEnd = matched[i][0].TargetEnd
				}
				mr[len(mr)-1] = append(prevMatchedRange(), matched[i][1:]...)
				continue
			}

			for j := 1; j < len(matched[i]); j++ {
				// Find the positions in the ranges where the
				// tail end of the previous matched range
				// overlaps with the start of the next matched
				// range.
				for k := len(prevMatchedRange()) - 1; k > 0; k-- {
					if prevMatchedRange()[k].SrcStart < matched[i][j].SrcStart &&
						prevMatchedRange()[k].TargetStart < matched[i][j].TargetStart {
						// Append the next range to the previous range.
						if prevMatchedRange()[k].TargetEnd < matched[i][j].TargetStart {
							// Coalesce the ranges.
							prevMatchedRange()[k].SrcEnd += matched[i][j-1].TargetEnd - prevMatchedRange()[k].TargetEnd
							prevMatchedRange()[k].TargetEnd = matched[i][j-1].TargetEnd
						}
						mr[len(mr)-1] = append(prevMatchedRange()[:k+1], matched[i][j:]...)
						continue NEXT
					}
				}
			}
		}
		mr = append(mr, matched[i])
	}
	return mr
}

// coalesceMatchRanges coalesces overlapping match ranges into a single
// contiguous match range.
func coalesceMatchRanges(matchedRanges MatchRanges) MatchRanges {
	coalesced := MatchRanges{matchedRanges[0]}
	for i := 1; i < len(matchedRanges); i++ {
		c := coalesced[len(coalesced)-1]
		mr := matchedRanges[i]

		if mr.SrcStart <= c.SrcEnd && mr.SrcStart >= c.SrcStart {
			var se, ts, te int
			if mr.SrcEnd > c.SrcEnd {
				se = mr.SrcEnd
			} else {
				se = c.SrcEnd
			}
			if mr.TargetStart < c.TargetStart {
				ts = mr.TargetStart
			} else {
				ts = c.TargetStart
			}
			if mr.TargetEnd > c.TargetEnd {
				te = mr.TargetEnd
			} else {
				te = c.TargetEnd
			}
			coalesced[len(coalesced)-1] = &MatchRange{
				SrcStart:    c.SrcStart,
				SrcEnd:      se,
				TargetStart: ts,
				TargetEnd:   te,
			}
		} else {
			coalesced = append(coalesced, mr)
		}
	}
	return coalesced
}
