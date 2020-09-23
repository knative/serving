/*
Copyright 2020 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package max

import (
	"fmt"

	"github.com/davecgh/go-spew/spew"
)

type entry struct {
	value float64
	index int
}

// window is a circular buffer which keeps track of the maximum value observed in a particular time.
// Based on the "ascending minima algorithm" (http://web.archive.org/web/20120805114719/http://home.tiac.net/~cri/2001/slidingmin.html).
type window struct {
	maxima        []entry
	first, length int
}

// newWindow creates an descending minima window buffer of size size.
func newWindow(size int) *window {
	return &window{
		maxima: make([]entry, size),
	}
}

// Record records a value for a monotonically increasing index.
func (m *window) Record(index int, v float64) {
	// Step One: Remove any elements where v > element.
	// An element that's lower than the new element can never influence the
	// maximum again, because the new element is both larger _and_ more
	// recent than it.
	for l := m.length - 1; l >= 0; l-- {
		// Search backwards because that way we can delete by just decrementing length.
		// The elements are guaranteed to be in descending order as described in Step Three.
		if v >= m.maxima[m.index(m.first+l)].value {
			m.length--
		} else {
			// The elements are sorted, no point continuing.
			break
		}
	}

	// Step Two: Remove out of date elements from front of array.
	// We only ever add at end of list, so the indexes are in ascending order,
	// therefore the oldest are always first.
	for m.length > 0 && index-m.maxima[m.first].index >= len(m.maxima) {
		m.length--
		m.first++

		// Circle around the buffer if necessary.
		if m.first == len(m.maxima) {
			m.first = 0
		}
	}

	// Step 2b: To be defensive against multiple values being recorded against
	// the same index, if the last index is the same as this one, we'll pick the largest.
	if m.length > 0 {
		if last := m.maxima[m.index(m.first+m.length-1)]; last.index == index {
			if last.value > v {
				v = last.value
			}

			// Remove last element because we'll add it back in Step Three.
			m.length--
		}
	}

	// Step Three: Add the new value to the end (which maintains sorted order
	// since we removed any lesser values above, so value we're appending is
	// always smallest value in list).
	m.maxima[m.index(m.first+m.length)] = entry{index: index, value: v}
	m.length++

	// We removed any items from the list in Step Two that were added more than
	// len(maxima) ago, so length can never be larger than len(maxima).
	if m.length > len(m.maxima) {
		panic(fmt.Sprintf("length %d exceeded buffer size %d. This should be impossible. Current state: %v", m.length, len(m.maxima), spew.Sdump(m)))
	}
}

// Current returns the current maximum value observed.
func (m *window) Current() float64 {
	return m.maxima[m.first].value
}

func (m *window) index(i int) int {
	return i % len(m.maxima)
}
