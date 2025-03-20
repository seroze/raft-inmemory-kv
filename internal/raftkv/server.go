package raftkv

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	LOCAL_IPADDR = "127.0.0.1"
)

func init() {
	gob.Register(RaftMessage{})
	gob.Register(AppendEntriesRequest{})
	gob.Register(AppendEntriesResponse{})
	gob.Register(VoteRequest{})
	gob.Register(VoteResponse{})
}

// Server represents a node in the Raft cluster
type Server struct {
	ID             int            // Unique server ID (1-5)
	IpAddr         string         // IpAddress
	Port           int            // port
	Logs           *Logs          // Log storage for RAFT replication
	Store          *Store         // The actual key-value store
	Peers          []Peer         // List of peer servers
	NodeAddressMap map[int]string // map of node ip:port to int
	currentTerm    int            // current term
	votedFor       int            // id of the server to whom the vote was given in current term
	commitIndex    int            // commit index
	matchIndex     map[int]int    // map of match index
	nextIndex      map[int]int    // map of next Index
	mu             sync.Mutex     // Mutex to prevent race conditions
	leader         bool           // If true, this server is the leader
}

type Peer struct {
	ID     int    // id of the peer. (1-5)
	IpAddr string // ip address of peer
	Port   int    // port
}

// NewServer initializes a new Server instance
func NewServer(id int, ipAddr string, port int, serverMap map[int]string) *Server {
	server := &Server{
		ID:             id,
		IpAddr:         LOCAL_IPADDR,
		Port:           port,
		Store:          NewStore(),
		Logs:           NewLogs(),
		Peers:          []Peer{}, // Empty list, will be populated later
		NodeAddressMap: serverMap,
		currentTerm:    0,
		commitIndex:    -1,
	}

	if server.ID == 1 {
		//set node 1 as leader for now, will remove this once leader election is in place
		server.SetLeader()
	}

	// Start listening for incoming messages
	go server.Listen()

	// Start periodic log printing
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			server.PrintLogs()
		}
	}()

	return server
}

func (s *Server) Listen() {
	address := fmt.Sprintf("%s:%d", s.IpAddr, s.Port)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		fmt.Printf("❌ Server %d failed to start listening on %s: %v\n", s.ID, address, err)
		return
	}
	defer listener.Close()

	fmt.Printf("✅ Server %d listening on %s...\n", s.ID, address)

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("❌ Error accepting connection:", err)
			continue
		}

		// Handle each connection in a separate goroutine
		go s.handleConnection(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()
	decoder := gob.NewDecoder(conn)

	for {
		var msg RaftMessage
		if err := decoder.Decode(&msg); err != nil {
			fmt.Println("Failed to decode RaftMessage:", err)
			return
		}

		fmt.Printf("Received RaftMessage: Type=%d, SenderID=%d\n", msg.Type, msg.SenderID)

		nodeID := msg.SenderID

		if _, exists := s.NodeAddressMap[nodeID]; !exists {
			fmt.Println("Unknown sender ID:", nodeID)
			return
		}

		switch data := msg.Data.(type) { // Type assertion
		case AppendEntriesRequest:
			s.handleAppendEntries(data, nodeID)

		case AppendEntriesResponse:
			s.handleAppendEntriesResponse(data, nodeID)

		// case VoteRequest:
		// 	s.handleVoteRequest(data, nodeID)

		// case VoteResponse:
		// 	s.handleVoteResponse(data, nodeID)

		default:
			fmt.Println("Unknown message type:", msg.Type)
		}
	}
}

func (s *Server) handleAppendEntries(req AppendEntriesRequest, nodeID int) AppendEntriesResponse {
	// if the current term in the request is lower, reject it
	if req.Term < s.currentTerm {
		return AppendEntriesResponse{
			Term:    s.currentTerm,
			Success: false,
		}
	}

	// update the current term if the leader has a higher term
	if req.Term > s.currentTerm {
		s.currentTerm = req.Term
		s.votedFor = req.LeaderID // update votedFor
	}

	// reset election timeout since we've received a valid heart beat
	// resetElectionTimer()
	//

	// check if the log contains an entry at prevLogIndex with matching prevLogTerm
	if req.PrevLogIndex >= 0 {
		if req.PrevLogIndex >= len(s.Logs.logs) || s.Logs.logs[req.PrevLogIndex].Term != req.PrevLogTerm {
			return AppendEntriesResponse{
				Term:    s.currentTerm,
				Success: false,
			}
		}
	}

	// check if the log contains an entry at prevLogIndex with matching prevLogTerm
	if len(req.Entries) > 0 {
		s.Logs.logs = s.Logs.logs[:req.PrevLogIndex+1] // Delete conflicting entries
		s.Logs.logs = append(s.Logs.logs, req.Entries...)
	}

	// update commit index
	if req.LeaderCommit > s.commitIndex {
		// it could be the case that follower is very far behind others
		s.commitIndex = min(req.LeaderCommit, len(s.Logs.logs)-1)
		s.applyCommittedEntries(req.Entries)
	}

	return AppendEntriesResponse{
		Term:    s.currentTerm,
		Success: true,
	}

}

func (s *Server) handleAppendEntriesResponse(res AppendEntriesResponse, fromID int) {
	if res.Success {
		// Update matchIndex and nextIndex for the follower
		s.matchIndex[fromID] = res.LastLogIndex
		s.nextIndex[fromID] = res.LastLogIndex + 1
		fmt.Printf("AppendEntries succeeded for %d, updated matchIndex: %d, nextIndex: %d\n",
			fromID, s.matchIndex[fromID], s.nextIndex[fromID])
	} else {
		// If the AppendEntries failed (log inconsistency), decrement nextIndex and retry
		if s.nextIndex[fromID] > 1 {
			s.nextIndex[fromID]--
		}
		fmt.Printf("AppendEntries failed for %d, decreasing nextIndex to %d\n",
			fromID, s.nextIndex[fromID])
		// Potentially retry sending AppendEntries with the updated nextIndex
	}
}

///////////////////////////////////////////////////////////////////////////////

func (s *Server) applyCommittedEntries(entries []Log) {
	for _, entry := range entries {
		s.ProcessCommand(entry.Command)
	}
}
func (s *Server) ProcessCommand(command string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	fmt.Printf("🔄 Processing command on Server %d: %s\n", s.ID, command)

	parts := strings.Split(command, " ")
	if len(parts) < 2 {
		fmt.Println("❌ Invalid command format")
		return
	}

	switch parts[0] {
	case "SET":
		if len(parts) < 3 {
			fmt.Println("❌ Invalid SET command")
			return
		}
		key, value := parts[1], parts[2]
		s.Store.Set(key, []byte(value))
		s.Logs.AppendLog(Log{Term: 1, Command: command})
		fmt.Printf("✅ Server %d SET %s = %s\n", s.ID, key, value)

	case "GET":
		key := parts[1]
		value, err := s.Store.Get(key)
		if err != nil {
			fmt.Printf("❌ Server %d: Key %s not found\n", s.ID, key)
		} else {
			fmt.Printf("✅ Server %d GET %s = %s\n", s.ID, key, string(value))
		}

	case "DELETE":
		key := parts[1]
		s.Store.Delete(key)
		s.Logs.AppendLog(Log{Term: 1, Command: command})
		fmt.Printf("✅ Server %d DELETED %s\n", s.ID, key)

	default:
		fmt.Println("❌ Unknown command:", command)
	}
}

// AddPeer adds a peer to the server's list of known nodes
// func (s *Server) AddPeer(peer Peer) {
// 	s.Peers = append(s.Peers, peer)
// }

// SetLeader makes this server the leader
func (s *Server) SetLeader() {
	s.leader = true
	fmt.Printf("Server %d is now the leader\n", s.ID)
}

///////////////////////////////////////////////////////////////////////////////
// AppendEntry applies an operation and replicates it to peers

func (s *Server) AppendEntry(command string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Create log entry
	logEntry := Log{Term: 1, Command: command}
	s.Logs.AppendLog(logEntry)

	// Leader should replicate log to peers
	if !s.leader {
		fmt.Printf("❌ Server %d is not the leader, skipping replication\n", s.ID)
		return
	}

	// Send log entry to all peers
	// for _, peer := range s.Peers {
	for peerID, peerIpPort := range s.NodeAddressMap {

		if peerID == s.ID {
			// Skip sending to self
			continue
		}

		// err := SendRPCMessage(peer, logEntry) // Send entire Log struct
		entries := make([]Log, 1)
		entries[0] = Log{
			Term:    s.currentTerm,
			Command: command,
		}

		prevLogIndex := len(s.Logs.logs) - 1 // Last log entry index
		prevLogTerm := 0                     // Default if no previous logs
		if prevLogIndex >= 0 {
			prevLogTerm = s.Logs.logs[prevLogIndex].Term
		}

		appendEntriesReq := AppendEntriesRequest{
			Term:         s.currentTerm,
			LeaderID:     s.ID,
			PrevLogIndex: prevLogIndex,
			PrevLogTerm:  prevLogTerm,
			Entries:      entries,
		}

		encodedData, err := Encode(appendEntriesReq)
		if err != nil {
			fmt.Println("Encoding error:", err)
			return
		}

		raftMessage := RaftMessage{
			Type:     AppendEntriesRPC,
			Data:     encodedData,
			SenderID: s.ID,
		}

		parts := strings.Split(peerIpPort, ":")

		myIP := parts[0]
		myPort, err := strconv.Atoi(parts[1])
		if err != nil {
			fmt.Printf("Error while parsing the port: %s\n", peerIpPort)
		}
		peer := Peer{
			ID:     s.ID,
			IpAddr: myIP,
			Port:   myPort,
		}

		err = sendMessage(peer, raftMessage)
		if err != nil {
			fmt.Printf("❌ Failed to send log to peer %d (%s:%d): %v\n", peer.ID, peer.IpAddr, peer.Port, err)
		}
	}
}

// // AppendEntries handles log replication from the leader
// func (s *Server) AppendEntries(args AppendEntriesArgs, reply AppendEntriesReply) error {
// 	s.mu.Lock()
// 	defer s.mu.Unlock()

// 	// Reject if leader's term is outdated
// 	if args.Term < s.currentTerm {
// 		reply.Term = s.currentTerm
// 		reply.Success = false
// 		return nil
// 	}

// 	// Update term if needed
// 	if args.Term > s.currentTerm {
// 		s.currentTerm = args.Term
// 		s.leader = false
// 	}

// 	// Check log consistency (PrevLogIndex and PrevLogTerm must match)
// 	if args.PrevLogIndex >= 0 && (len(s.Logs.logs) <= args.PrevLogIndex || s.Logs.logs[args.PrevLogIndex].Term != args.PrevLogTerm) {
// 		reply.Success = false
// 		return nil
// 	}

// 	// Append new log entries
// 	s.Logs.logs = append(s.Logs.logs[:args.PrevLogIndex+1], args.Entries...)
// 	s.commitIndex = args.LeaderCommit

// 	reply.Term = s.currentTerm
// 	reply.Success = true
// 	return nil
// }

///////////////////////////////////////////////////////////////////////////////

// // RequestVote handles vote requests from candidates
// func (s *Server) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) error {
// 	s.mu.Lock()
// 	defer s.mu.Unlock()

// 	// Reject if the candidate's term is outdated
// 	if args.Term < s.currentTerm {
// 		reply.Term = s.currentTerm
// 		reply.VoteGranted = false
// 		return nil
// 	}

// 	// If the candidate has a newer term, update term and reset vote
// 	if args.Term > s.currentTerm {
// 		s.currentTerm = args.Term
// 		s.votedFor = -1
// 	}

// 	// Check if we can grant the vote
// 	if (s.votedFor == -1 || s.votedFor == args.CandidateID) &&
// 		(args.LastLogTerm > s.getLastLogTerm() || (args.LastLogTerm == s.getLastLogTerm() && args.LastLogIndex >= s.getLastLogIndex())) {
// 		s.votedFor = args.CandidateID
// 		reply.VoteGranted = true
// 	} else {
// 		reply.VoteGranted = false
// 	}

// 	reply.Term = s.currentTerm
// 	return nil
// }

// func (s *Server) getLastLogIndex() int {
// 	panic("unimplemented")
// }

// func (s *Server) getLastLogTerm() int {
// 	panic("unimplemented")
// }

///////////////////////////////////////////////////////////////////////////////

// Get retrieves a value for a given key and logs the operation
func (s *Server) Get(key string) ([]byte, error) {
	val, err := s.Store.Get(key)
	if err != nil {
		return nil, err
	}

	// Log the GET operation (optional, since GETs are read-only)
	logEntry := Log{Term: 1, Command: fmt.Sprintf("GET %s", key)}
	s.AppendEntry(logEntry.String())

	return val, nil
}

// Set adds or updates a key-value pair and logs the operation
func (s *Server) Set(key string, val []byte) {
	// Log the SET operation
	logEntry := Log{Term: 1, Command: fmt.Sprintf("SET %s %s", key, string(val))}
	s.AppendEntry(logEntry.String())
	s.Store.Set(key, val)

}

// Delete removes a key from the store and logs the operation
func (s *Server) Delete(key string) {
	// Log the DELETE operation
	logEntry := Log{Term: 1, Command: fmt.Sprintf("DELETE %s", key)}
	s.AppendEntry(logEntry.String())

	s.Store.Delete(key)
}

// PrintLogs displays all logs (for debugging)
func (s *Server) PrintLogs() {
	s.mu.Lock()
	defer s.mu.Unlock()

	fmt.Printf("Logs for Server %d:\n", s.ID)
	for i, log := range s.Logs.logs {
		fmt.Printf("[%d] Term: %d, Command: %s\n", i, log.Term, log.Command)
	}
}

func Decode(data []byte, msg interface{}) error {
	buf := bytes.NewBuffer(data)
	dec := gob.NewDecoder(buf)
	return dec.Decode(msg)
}
