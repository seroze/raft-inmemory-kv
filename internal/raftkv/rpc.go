package raftkv

import (
	"encoding/gob"
	"fmt"
	"net"
	"time"
)

const (
	RPC_TIMEOUT = 3 * time.Second
)

func sendMessage(peer Peer, msg RaftMessage) error {

	// create conn object
	address := fmt.Sprintf("%s:%d", peer.IpAddr, peer.Port)
	conn, err := net.DialTimeout("tcp", address, RPC_TIMEOUT)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %v", address, err)
	}
	defer conn.Close()

	encoder := gob.NewEncoder(conn)
	return encoder.Encode(msg)
}

// message type
type MessageType uint8

const (
	AppendEntriesRPC MessageType = iota
	VoteRPC
	VoteResponseRPC
	AppendEntriesResponseRPC
)

type RaftMessage struct {
	Type MessageType
	// Data     []byte // message encoded as a payload
	Data     interface{}
	SenderID int // sender id
}

// AppendEntriesArgs represents the arguments sent in AppendEntries RPC
type AppendEntriesRequest struct {
	Term         int   // Leader’s term
	LeaderID     int   // Leader’s ID
	PrevLogIndex int   // Index of log entry immediately before new ones
	PrevLogTerm  int   // Term of PrevLogIndex entry
	Entries      []Log // Log entries to store (empty for heartbeat)
	LeaderCommit int   // Leader’s commit index
}

// Vote Request
type VoteRequest struct {
	Term         int
	CandidateID  int
	LastLogIndex int
	LastLogTerm  int
}

// Vote Response
type VoteResponse struct {
	Term        int
	VoteGranted bool
}

// AppendEntriesResponse
type AppendEntriesResponse struct {
	Term         int
	Success      bool
	LastLogIndex int // this is not mentioned in paper but having this makes life easy
}
