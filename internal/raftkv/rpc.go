package raftkv

import (
	"bytes"
	// "encoding/binary"
	"encoding/gob"
	"fmt"
	"net"
	"time"
)

const (
	RPC_TIMEOUT = 3 * time.Second
)

// One silly thing here is that you can only send messages that fit within a tcp buffer
// encodeMessage encodes a string as [4-byte length] + [command string]
// func encodeMessage(message string) ([]byte, error) {
// 	var buf bytes.Buffer

// 	// Write message length (big-endian uint32)
// 	length := uint32(len(message))
// 	if err := binary.Write(&buf, binary.BigEndian, length); err != nil {
// 		return nil, err
// 	}

// 	// Write the actual command
// 	buf.WriteString(message)

// 	return buf.Bytes(), nil
// }

// SendRPCMessage encodes and sends a Log entry to a peer
func SendRPCMessage(peer Peer, logEntry Log) error {
	address := fmt.Sprintf("%s:%d", peer.IpAddr, peer.Port)
	conn, err := net.DialTimeout("tcp", address, RPC_TIMEOUT)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %v", address, err)
	}
	defer conn.Close()

	// Encode Log struct using GOB
	var buffer bytes.Buffer
	encoder := gob.NewEncoder(&buffer)
	err = encoder.Encode(logEntry)
	if err != nil {
		return fmt.Errorf("failed to encode log entry: %v", err)
	}

	// Send encoded log entry
	_, err = conn.Write(buffer.Bytes())
	if err != nil {
		return fmt.Errorf("failed to send message: %v", err)
	}

	fmt.Printf("📤 Sent log entry to peer %d: %+v\n", peer.ID, logEntry)
	return nil
}
