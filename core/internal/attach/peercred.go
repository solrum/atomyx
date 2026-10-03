package attach

import (
	"fmt"
	"net"
	"os"
)

// PeerCredFunc reports the uid of the process at the other end of a unix
// connection. A server and a client take one so a test can stand in for a
// peer with another uid.
type PeerCredFunc func(*net.UnixConn) (uint32, error)

// PeerUIDError reports a peer whose uid is not the uid of this process.
type PeerUIDError struct {
	Got, Want uint32
}

func (e *PeerUIDError) Error() string {
	return fmt.Sprintf("attach: peer uid %d is not %d", e.Got, e.Want)
}

// VerifyPeer returns a *PeerUIDError unless the peer of c runs as the
// current user. A nil cred reads the platform credentials.
func VerifyPeer(c *net.UnixConn, cred PeerCredFunc) error {
	if cred == nil {
		cred = platformPeerUID
	}
	got, err := cred(c)
	if err != nil {
		return err
	}
	want := uint32(os.Getuid())
	if got != want {
		return &PeerUIDError{Got: got, Want: want}
	}
	return nil
}
