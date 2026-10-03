package lease

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHolderJSON_OmitsSocketErrorUnlessSet(t *testing.T) {
	plain, err := json.Marshal(Holder{PID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "socketError") {
		t.Fatalf("holder JSON %s carries socketError, want it omitted when empty", plain)
	}
	failed, err := json.Marshal(Holder{PID: 1, SocketError: "bind failed"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(failed), `"socketError":"bind failed"`) {
		t.Fatalf("holder JSON %s lacks the socketError field", failed)
	}
}
