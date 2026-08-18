package protocol

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const Version = 1

type Message struct {
	Type            string `json:"type"`
	ProtocolVersion int    `json:"protocol_version,omitempty"`
	NodeID          string `json:"node_id,omitempty"`
	Token           string `json:"token,omitempty"`
	VirtualIP       string `json:"virtual_ip,omitempty"`
	NetworkCIDR     string `json:"network_cidr,omitempty"`
	Error           string `json:"error,omitempty"`
	Payload         []byte `json:"payload,omitempty"`
	TimestampUnix   int64  `json:"timestamp_unix,omitempty"`
}

func Send(w io.Writer, msg Message) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if len(b) > 1<<20 {
		return fmt.Errorf("message too large: %d", len(b))
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(b)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
	}

func Receive(r *bufio.Reader, maxFrameSize int) (Message, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return Message{}, err
	}
	length := int(binary.BigEndian.Uint32(header[:]))
	if length <= 0 || length > maxFrameSize {
		return Message{}, errors.New("invalid frame length")
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return Message{}, err
	}
	var msg Message
	if err := json.Unmarshal(payload, &msg); err != nil {
		return Message{}, err
	}
	return msg, nil
	}
