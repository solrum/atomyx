package runner

import (
	crand "crypto/rand"
	"encoding/binary"
	"math/rand/v2"
	"sync"
)

// RandomSource supplies the randomness behind inputRandom steps. A run
// that needs repeatable values injects a seeded source through
// ScriptRunnerDeps.Random, so the generated text can be asserted.
type RandomSource interface {
	// IntN returns a value in [0, n). n must be positive.
	IntN(n int) int
}

// NewSeededRandom returns a RandomSource whose sequence depends only on
// seed. It is safe for concurrent use and never touches the process-wide
// random state.
func NewSeededRandom(seed uint64) RandomSource {
	return &lockedRandom{rng: rand.New(rand.NewPCG(seed, seed^pcgSecondWordMask))}
}

// pcgSecondWordMask derives the second PCG state word from the seed, so
// one number fully determines a sequence.
const pcgSecondWordMask = 0x9e3779b97f4a7c15

// newEntropyRandom returns a RandomSource seeded from the operating
// system's entropy, for runs that do not inject their own.
func newEntropyRandom() RandomSource {
	var seed [8]byte
	if _, err := crand.Read(seed[:]); err != nil {
		panic(err)
	}
	return NewSeededRandom(binary.LittleEndian.Uint64(seed[:]))
}

type lockedRandom struct {
	mu  sync.Mutex
	rng *rand.Rand
}

func (r *lockedRandom) IntN(n int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rng.IntN(n)
}
