package orchestra

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"dev.atomyx/internal/wire"
)

// Pixel size of the bucket each bounds coordinate is rounded into
// before hashing. Picked larger than the typical iOS UIScrollView
// rubber-band settle drift (usually under 30 px) so a bounce on a
// non-scrollable container does not appear as a content change. Real
// list scrolls move items by at least one row height (at least 40 px
// on common layouts), so legitimate scrolling crosses bucket
// boundaries reliably.
const boundsBucketPx = 50

// HashVisibleLeaves hashes the set of visible leaf nodes in a UI
// tree. A leaf is any node with no children. Each leaf contributes a
// string combining its text, label, and bucketed bounds attributes;
// leaf strings are sorted before hashing so sibling order does not
// affect the result, only the SET of visible leaves.
//
// Bounds are quantized into boundsBucketPx cells before hashing so
// sub-bucket drift (a rubber-band bounce settling back near its
// original position) does not flip the hash. Two consecutive
// snapshots of the same content land in the same buckets even when
// the platform reports bounds off by a few pixels.
//
// Callers compare two consecutive hashes to detect scroll-boundary
// saturation: equal hashes mean the tree did not change after a
// swipe (a boundary hit or a non-scrollable container), so further
// swipes in that direction are pointless.
func HashVisibleLeaves(tree wire.TreeNodeWire) string {
	var parts []string
	collectLeaves(tree, &parts)
	sort.Strings(parts)
	return djb2(strings.Join(parts, "\x00"))
}

// CountLeaves counts leaf nodes in tree. Used for the scroll-search
// viewport pre-check: a tree with very few leaves that also occupies
// little vertical space is likely a lazy-loaded or non-scrollable
// container, so the swipe budget is capped early.
func CountLeaves(tree wire.TreeNodeWire) int {
	if len(tree.Children) == 0 {
		return 1
	}
	count := 0
	for _, child := range tree.Children {
		count += CountLeaves(child)
	}
	return count
}

// MaxLeafBoundsBottom returns the maximum bottom edge (from the
// "left,top,right,bottom" bounds attribute) across every leaf node in
// tree. Returns 0 when no leaf carries a parseable bounds attribute.
// Used alongside CountLeaves to detect a thin tree.
func MaxLeafBoundsBottom(tree wire.TreeNodeWire) float64 {
	max := 0.0
	visitLeaves(tree, func(n wire.TreeNodeWire) {
		bounds := n.Attributes[AttrBounds]
		if bounds == "" {
			return
		}
		parts := strings.Split(bounds, ",")
		if len(parts) != 4 {
			return
		}
		bottom, err := strconv.ParseFloat(strings.TrimSpace(parts[3]), 64)
		if err != nil {
			return
		}
		if bottom > max {
			max = bottom
		}
	})
	return max
}

func collectLeaves(node wire.TreeNodeWire, out *[]string) {
	if len(node.Children) == 0 {
		text := node.Attributes[AttrText]
		label := node.Attributes[AttrLabel]
		bounds := node.Attributes[AttrBounds]
		*out = append(*out, text+"|"+label+"|"+BucketBounds(bounds))
		return
	}
	for _, child := range node.Children {
		collectLeaves(child, out)
	}
}

// BucketBounds quantizes a "left,top,right,bottom" bounds attribute
// into boundsBucketPx cells, one component at a time. A component
// that fails to parse as a number is kept as its original string.
// Used by HashVisibleLeaves to make the hash tolerant of sub-bucket
// bounds drift; exported directly so the quantization itself
// (including its negative-coordinate rounding behavior) is testable
// independent of the hashing pipeline.
func BucketBounds(bounds string) string {
	if bounds == "" {
		return ""
	}
	parts := strings.Split(bounds, ",")
	if len(parts) != 4 {
		return bounds
	}
	out := make([]string, 4)
	for i, p := range parts {
		n, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			out[i] = p
			continue
		}
		out[i] = strconv.Itoa(int(math.Floor(n / boundsBucketPx)))
	}
	return strings.Join(out, ",")
}

func visitLeaves(node wire.TreeNodeWire, cb func(wire.TreeNodeWire)) {
	if len(node.Children) == 0 {
		cb(node)
		return
	}
	for _, child := range node.Children {
		visitLeaves(child, cb)
	}
}

// djb2 hashes s starting from 5381, folding one byte at a time as
// ((hash << 5) + hash) ^ byte, kept unsigned 32-bit. Returns the
// result as a lowercase hex string.
func djb2(s string) string {
	var hash uint32 = 5381
	for i := 0; i < len(s); i++ {
		hash = ((hash << 5) + hash) ^ uint32(s[i])
	}
	return strconv.FormatUint(uint64(hash), 16)
}
