package protocol

import (
	"bufio"
	"bytes"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	in := Message{Type: "HELLO", ProtocolVersion: 1, NodeID: "node-a"}
	if err := Send(&buf, in); err != nil {
		t.Fatal(err)
	}
	out, err := Receive(bufio.NewReader(&buf), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if out.Type != in.Type || out.NodeID != in.NodeID {
		t.Fatalf("unexpected message: %#v", out)
	}
}
