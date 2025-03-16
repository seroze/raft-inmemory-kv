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

// AppendEntriesArgs represents the arguments sent in AppendEntries RPC
type AppendEntriesArgs struct {
	Term         int    // Leader’s term
	LeaderID     int    // Leader’s ID
	PrevLogIndex int    // Index of log entry immediately before new ones
	PrevLogTerm  int    // Term of PrevLogIndex entry
	Entries      []Log  // Log entries to store (empty for heartbeat)
	LeaderCommit int    // Leader’s commit index
}

// AppendEntriesReply represents the response from followers
type AppendEntriesReply struct {
	Term    int  // Current term (for leader to update)
	Success bool // True if follower appended logs successfully
}

// RequestVoteArgs represents the arguments for a vote request
type RequestVoteArgs struct {
	Term         int // Candidate’s term
	CandidateID  int // Candidate requesting vote
	LastLogIndex int // Index of candidate’s last log entry
	LastLogTerm  int // Term of candidate’s last log entry
}

// RequestVoteReply represents the response from followers
type RequestVoteReply struct {
	Term        int  // Current term (for candidate to update)
	VoteGranted bool // True if candidate received vote
}
