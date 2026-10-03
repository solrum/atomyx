package inspector

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"sync"

	"dev.atomyx/internal/clock"
)

const (
	// tokenHeader carries the session token on every /api/ request.
	tokenHeader = "X-Atomyx-Token"
	// secretBytes is the entropy of a bootstrap code and of a token.
	secretBytes = 32
	// codeLifetimeMs is how long a bootstrap code stays valid after it
	// is minted. A code older than this is refused and discarded.
	codeLifetimeMs = 60_000
)

// exchangeOutcome says why an exchange did or did not issue a token.
type exchangeOutcome int

const (
	exchangeIssued exchangeOutcome = iota
	// exchangeRefused covers a wrong code and a code already spent.
	exchangeRefused
	exchangeExpired
)

type digest [sha256.Size]byte

// authority issues the single-use bootstrap code and the session
// tokens it is exchanged for. It stores only SHA-256 digests, and
// compares a presented secret against them in constant time.
type authority struct {
	entropy io.Reader
	clk     clock.Clock

	mu sync.Mutex
	// code is the digest of the one code not yet exchanged. Minting a
	// new code replaces it, so a URL that was never opened cannot be
	// used after a fresh one is printed.
	code *digest
	// codeMintedMs is when code was minted, on clk.
	codeMintedMs int64
	// tokens holds the digest of every token issued. A token stays
	// valid until the server stops, because a reload that mints a new
	// code must not cut off a tab that is still open.
	tokens []digest
}

func newAuthority(entropy io.Reader, clk clock.Clock) *authority {
	return &authority{entropy: entropy, clk: clk}
}

func (a *authority) newSecret() (string, digest, error) {
	buf := make([]byte, secretBytes)
	if _, err := io.ReadFull(a.entropy, buf); err != nil {
		return "", digest{}, fmt.Errorf("read entropy: %w", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(buf)
	return secret, sha256.Sum256([]byte(secret)), nil
}

// mintCode returns a fresh bootstrap code and invalidates any earlier
// code that was not exchanged. Tokens already issued are untouched.
func (a *authority) mintCode() (string, error) {
	secret, sum, err := a.newSecret()
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.code = &sum
	a.codeMintedMs = a.clk.NowMs()
	return secret, nil
}

// exchange spends code for a token. A wrong code leaves the pending
// code usable. A pending code older than codeLifetimeMs is discarded
// and reported as expired, whichever code was presented, so the
// operator is told to ask for a fresh URL rather than to retry.
func (a *authority) exchange(code string) (string, exchangeOutcome, error) {
	presented := sha256.Sum256([]byte(code))
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.code == nil {
		return "", exchangeRefused, nil
	}
	if a.clk.NowMs()-a.codeMintedMs > codeLifetimeMs {
		a.code = nil
		return "", exchangeExpired, nil
	}
	if subtle.ConstantTimeCompare(presented[:], a.code[:]) != 1 {
		return "", exchangeRefused, nil
	}
	secret, sum, err := a.newSecret()
	if err != nil {
		return "", exchangeRefused, err
	}
	a.code = nil
	a.tokens = append(a.tokens, sum)
	return secret, exchangeIssued, nil
}

// valid reports whether token was issued by exchange. It visits every
// issued token so the time taken does not reveal which one matched.
func (a *authority) valid(token string) bool {
	presented := sha256.Sum256([]byte(token))
	a.mu.Lock()
	defer a.mu.Unlock()
	match := 0
	for i := range a.tokens {
		match |= subtle.ConstantTimeCompare(presented[:], a.tokens[i][:])
	}
	return match == 1
}
