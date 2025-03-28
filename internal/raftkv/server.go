package raftkv

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	LOCAL_IPADDR       = "127.0.0.1"
	ElectionTimeoutMin = 150 * time.Millisecond
	ElectionTimeoutMax = 300 * time.Millisecond
	HeartBeatInterval  = 50 * time.Millisecond
)

// go doesn't support enum keyword but you can define it via constants and iota
type State int

const (
	Follower State = iota
	Candidate
	Leader
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
	NodeAddressMap map[int]string // map of node ip:port to int
	currentTerm    int            // current term
	commitIndex    int            // commit index
	matchIndex     map[int]int    // map of match index
	nextIndex      map[int]int    // map of next Index
	mu             sync.Mutex     // Mutex to prevent race conditions
	leader         bool           // If true, this server is the leader

	// election related logic
	state                State
	votedFor             int // id of the server to whom the vote was given in current term
	votesReceived        int32
	electionTimer        *time.Timer
	electionTimeout      time.Duration
	resetElectionTimerCh chan struct{}
	stopCh               chan struct{} // used for graceful shutdown
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
		NodeAddressMap: serverMap,
		currentTerm:    1,
		commitIndex:    -1,

		// election related logic
		state:                Follower,
		votedFor:             -1, // currently pointing to no one
		votesReceived:        0,
		leader:               false,
		resetElectionTimerCh: make(chan struct{}),
		stopCh:               make(chan struct{}),
	}

	// Initialize election timer
	server.resetElectionTimer() // This is crucial

	go server.runElectionTimer()

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

func (s *Server) startElection() {
	// s.mu.Lock()
	// defer s.mu.Unlock()

	// // Some basic checks
	// // don't start an election if we're not a follower or candidate
	// if s.state == Leader {
	// 	// it means we are not follower or candidate
	// 	return
	// }

	// // change state
	// s.state = Candidate
	// // increase currentTerm
	// s.currentTerm++
	// // vote for self
	// s.votedFor = s.ID

	// // persist state information
	// // s.persistState()
	// //

	// // prepare vote request object
	// // var lastLogIndex, lastLogTerm int
	lastLogIndex, lastLogTerm := s.getLastLogAndLastTerm()
	voteRequest := VoteRequest{
		Term:         s.currentTerm,
		CandidateID:  s.ID,
		LastLogIndex: lastLogIndex,
		LastLogTerm:  lastLogTerm,
	}
	raftMessage := RaftMessage{
		Type:     VoteRPC,
		Data:     voteRequest,
		SenderID: s.ID,
	}

	// // Unlock while waiting for RPC responses
	// s.mu.Unlock()

	// // Send RequestVote RPCs to all other servers in parallel
	// // Start with 1 for self-vote
	// atomic.AddInt32(&s.votesReceived, 1)

	var wg sync.WaitGroup

	s.mu.Lock()
	defer s.mu.Unlock() // Use defer to ensure unlock happens

	if s.state == Leader {
		return
	}

	s.state = Candidate
	s.currentTerm++
	s.votedFor = s.ID
	atomic.StoreInt32(&s.votesReceived, 1) // Reset vote count

	// Send log entry to all peers
	// for _, peer := range s.Peers {
	for peerID, peerIpPort := range s.NodeAddressMap {
		if peerID == s.ID {
			// Skip sending to self
			continue
		}
		wg.Add(1)
		go func(peerIpPort string) {
			defer wg.Done()

			// reply := RequestVoteReply
			parts := strings.Split(peerIpPort, ":")

			peerIP := parts[0]
			peerPort, err := strconv.Atoi(parts[1])
			if err != nil {
				fmt.Printf("Error while parsing the port: %s\n", peerIpPort)
			}
			peer := Peer{
				ID:     peerID,
				IpAddr: peerIP,
				Port:   peerPort,
			}

			err = sendMessage(peer, raftMessage)
			if err != nil {
				fmt.Printf("❌ Failed to send vote request to peer %d (%s:%d): %v\n", peer.ID, peer.IpAddr, peer.Port, err)
			}
		}(peerIpPort)
	}

	// Wait for all RPCs to complete (or timeout)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All  RPCs completed
	case <-time.After(s.electionTimeout):
		// Election timed out
	}

	s.mu.Lock()
	defer s.mu.Unlock()
}

func (s *Server) startHeartBeats() {

	if s.state != Leader {
		// Only leaders send heart beat
		return
	}

	// Typically much shorter than election timeout
	ticker := time.NewTicker(HeartBeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if s.state != Leader {
				return // Stop if we're no longer leader
			}
			s.sendHeartBeat()
		case <-s.stopCh:
			return // Stop on shutdown signal
		}
	}
}

// Sends heart beat to all it's peers
func (s *Server) sendHeartBeat() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for peerID, peerIpPort := range s.NodeAddressMap {
		if peerID == s.ID {
			continue // Skip self
		}

		// Get the nextIndex for this peer
		nextIdx, exists := s.nextIndex[peerID]
		if !exists {
			nextIdx = 0 // Initialize if not exists
			s.nextIndex[peerID] = 0
			s.matchIndex[peerID] = -1
		}

		// Prepare the AppendEntries RPC arguments
		prevLogIndex := nextIdx - 1
		prevLogTerm := -1
		if prevLogIndex >= 0 && prevLogIndex < len(s.Logs.logs) {
			prevLogTerm = s.Logs.logs[prevLogIndex].Term
		}

		// For heartbeats, we send empty entries (unless we need to replicate)
		entries := []Log{}
		if nextIdx < len(s.Logs.logs) {
			// There are new entries to replicate
			entries = s.Logs.logs[nextIdx:]
		}

		req := AppendEntriesRequest{
			Term:         s.currentTerm,
			LeaderID:     s.ID,
			PrevLogIndex: prevLogIndex,
			PrevLogTerm:  prevLogTerm,
			Entries:      entries,
			LeaderCommit: s.commitIndex,
		}

		// Send the heartbeat in a goroutine so we don't block
		go func(peerID int, peerIpPort string, req AppendEntriesRequest) {
			parts := strings.Split(peerIpPort, ":")
			peerIP := parts[0]
			peerPort, _ := strconv.Atoi(parts[1])

			peer := Peer{
				ID:     peerID,
				IpAddr: peerIP,
				Port:   peerPort,
			}

			raftMessage := RaftMessage{
				Type:     AppendEntriesRPC,
				Data:     req,
				SenderID: s.ID,
			}

			err := sendMessage(peer, raftMessage)
			if err != nil {
				fmt.Printf("❌ Failed to send heartbeat to peer %d: %v\n", peerID, err)
			}
		}(peerID, peerIpPort, req)
	}
}

func (s *Server) resetElectionTimer() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Calculate random duration between min and max
	timeoutRange := ElectionTimeoutMax - ElectionTimeoutMin
	randomOffset := time.Duration(rand.Int63n(int64(timeoutRange)))
	s.electionTimeout = ElectionTimeoutMin + randomOffset

	// Initialize or reset timer
	if s.electionTimer == nil {
		s.electionTimer = time.NewTimer(s.electionTimeout)
	} else {
		if !s.electionTimer.Stop() {
			select {
			case <-s.electionTimer.C:
			default:
			}
		}
		s.electionTimer.Reset(s.electionTimeout)
	}
}

func (s *Server) runElectionTimer() {
	for {
		select {
		case <-s.electionTimer.C:
			// Election timeout elapsed, start an election
			s.startElection()
		case <-s.resetElectionTimerCh:
			// reset the election timer
			s.resetElectionTimer()
		case <-s.stopCh:
			// Stop the election timer
			if s.electionTimer != nil {
				s.electionTimer.Stop()
			}
			return
		}
	}
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

	// for {
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

	switch msg.Type { // Type assertion
	case AppendEntriesRPC:
		data := msg.Data.(AppendEntriesRequest)

		response := s.handleAppendEntries(data, nodeID)

		// Send response back to leader
		replyMsg := RaftMessage{
			Type:     AppendEntriesResponseRPC,
			Data:     response,
			SenderID: s.ID,
		}
		s.sendRaftMessage(nodeID, replyMsg)

	case AppendEntriesResponseRPC:
		data := msg.Data.(AppendEntriesResponse)
		s.handleAppendEntriesResponse(data, nodeID)

	case VoteRPC:
		data := msg.Data.(VoteRequest)
		response := s.handleVoteRequest(data, nodeID)
		// Send response back to the candidate
		replyMsg := RaftMessage{
			Type:     VoteResponseRPC,
			Data:     response,
			SenderID: s.ID,
		}
		s.sendRaftMessage(nodeID, replyMsg)

	case VoteResponseRPC:
		data := msg.Data.(VoteResponse)
		s.handleVoteResponse(data, nodeID)

	default:
		fmt.Println("Unknown message type:", msg.Type)
	}
	// }
}

func (s *Server) handleVoteRequest(data VoteRequest, nodeID int) VoteResponse {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Initialize response with current term
	voteResponse := VoteResponse{
		Term:        s.currentTerm,
		VoteGranted: false,
	}

	// Rule 1: If request term is older than our current term, reject
	if s.currentTerm > data.Term {
		return voteResponse
	}

	// Rule 2: If request term is newer, update our term and convert to follower
	if data.Term > s.currentTerm {
		s.currentTerm = data.Term
		s.state = Follower
		s.votedFor = -1
		// Note: Should persist state here in a real implementation
		voteResponse.Term = data.Term
	}

	// Rule 3: Check voting eligibility
	lastLogIndex, lastLogTerm := s.getLastLogAndLastTerm()
	candidateLogOK := (data.LastLogTerm > lastLogTerm) ||
		(data.LastLogTerm == lastLogTerm && data.LastLogIndex >= lastLogIndex)

	canVote := (s.votedFor == -1 || s.votedFor == nodeID) && candidateLogOK

	// Rule 4: Grant vote if:
	// - We haven't voted for anyone else this term
	// - Candidate's log is at least as up-to-date as ours
	if canVote {
		s.votedFor = nodeID
		s.resetElectionTimer() // Reset our election timeout when granting vote
		voteResponse.VoteGranted = true
		// Note: Should persist state here in a real implementation
	}

	return voteResponse
}

func (s *Server) handleVoteResponse(res VoteResponse, fromID int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Debug logging
	fmt.Printf("Server %d processing vote response from %d: granted=%v, term=%d (current term=%d)\n",
		s.ID, fromID, res.VoteGranted, res.Term, s.currentTerm)

	// 1. If response term > currentTerm, step down to follower
	if res.Term > s.currentTerm {
		fmt.Printf("Server %d found newer term in vote response, stepping down\n", s.ID)
		s.currentTerm = res.Term
		s.state = Follower
		s.votedFor = -1
		s.resetElectionTimer()
		return
	}

	// 2. Ignore if we're not candidate or terms don't match
	if s.state != Candidate || res.Term != s.currentTerm {
		fmt.Printf("Server %d ignoring vote response - not candidate or term mismatch\n", s.ID)
		return
	}

	// 3. Count the vote if granted
	if res.VoteGranted {
		votes := atomic.AddInt32(&s.votesReceived, 1)
		fmt.Printf("Server %d received vote from %d (total votes=%d)\n",
			s.ID, fromID, votes)
	}

	// 4. Check if we've won the election
	majority := len(s.NodeAddressMap)/2 + 1
	currentVotes := int(atomic.LoadInt32(&s.votesReceived))

	if currentVotes >= majority {
		fmt.Printf("Server %d achieved majority with %d votes (needed %d)\n",
			s.ID, currentVotes, majority)

		// Transition to leader
		s.state = Leader
		s.leader = true
		s.votedFor = -1

		// Initialize leader state
		lastLogIndex, _ := s.getLastLogAndLastTerm()
		s.nextIndex = make(map[int]int)
		s.matchIndex = make(map[int]int)

		for peerID := range s.NodeAddressMap {
			s.nextIndex[peerID] = lastLogIndex + 1
			s.matchIndex[peerID] = 0
		}

		// Send initial empty AppendEntries as heartbeat
		go s.startHeartBeats()

		fmt.Printf("Server %d is now LEADER for term %d\n", s.ID, s.currentTerm)
	} else {
		remainingVotes := majority - currentVotes
		fmt.Printf("Server %d needs %d more votes to become leader\n",
			s.ID, remainingVotes)
	}
}

func (s *Server) handleAppendEntries(req AppendEntriesRequest, nodeID int) AppendEntriesResponse {
	fmt.Println("Received appendEntriesRequest ", req)
	// if the current term in the request is lower, reject it
	if req.Term < s.currentTerm {
		fmt.Println("request term is lower than currentTerm")
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
	s.resetElectionTimer()
	//

	// check if the log contains an entry at prevLogIndex with matching prevLogTerm
	fmt.Printf("prev log index %d prev log term %d\n", req.PrevLogIndex, req.PrevLogTerm)
	if req.PrevLogIndex >= 0 && req.PrevLogIndex <= len(s.Logs.logs)-1 {
		fmt.Println(s.Logs.logs, " logs")
		if s.Logs.logs[req.PrevLogIndex].Term != req.PrevLogTerm {
			fmt.Println("Returning early")
			return AppendEntriesResponse{
				Term:         s.currentTerm,
				Success:      false,
				LastLogIndex: len(s.Logs.logs),
			}
		}
	}

	// check if the log contains an entry at prevLogIndex with matching prevLogTerm
	if len(req.Entries) > 0 {
		fmt.Println(req.Entries, " req.Entries")
		s.Logs.logs = s.Logs.logs[:req.PrevLogIndex+1] // Delete conflicting entries
		s.Logs.logs = append(s.Logs.logs, req.Entries...)
	}

	// update commit index
	if req.LeaderCommit > s.commitIndex {
		// it could be the case that follower is very far behind others
		s.commitIndex = min(req.LeaderCommit, len(s.Logs.logs)-1)
		s.applyCommittedEntries(req.Entries)
	}

	//for now let's just apply committed entries
	s.applyCommittedEntries(req.Entries)

	return AppendEntriesResponse{
		Term:         s.currentTerm,
		Success:      true,
		LastLogIndex: len(s.Logs.logs),
	}
}

func (s *Server) handleAppendEntriesResponse(res AppendEntriesResponse, fromID int) {
	if res.Success {
		// Is this correct ?
		s.matchIndex[fromID] = res.LastLogIndex
		// Update nextIndex
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
	fmt.Println("Applying committed Entries")

	for _, entry := range entries {
		fmt.Printf("🔄 Processing raw command on Server %d: %q\n", s.ID, entry.Command) // Log with quotes
		fmt.Printf("Command type: %T, Value: %v\n", entry.Command, entry.Command)

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
		s.Logs.AppendLog(Log{Term: s.currentTerm, Command: command})
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
		s.Logs.AppendLog(Log{Term: s.currentTerm, Command: command})
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
	s.state = Leader
	fmt.Printf("Server %d is now the leader\n", s.ID)
}

///////////////////////////////////////////////////////////////////////////////
// AppendEntry applies an operation and replicates it to peers

func (s *Server) AppendEntry(logEntry Log) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Create log entry
	// logEntry := Log{Term: 1, Command: command}
	// Append command to own log
	s.Logs.AppendLog(logEntry)

	// Leader should replicate log to peers
	if s.state != Leader {
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

		fmt.Printf("Sending command to %d %s\n", peerID, peerIpPort)

		// err := SendRPCMessage(peer, logEntry) // Send entire Log struct
		lastMatchindex, exists := s.nextIndex[peerID]
		if !exists {
			lastMatchindex = -1
		}
		// from lastMatchIndex+1 to whatever is there
		entries := s.Logs.logs[lastMatchindex+1:]

		prevLogIndex := lastMatchindex // Last log entry index, since we have already
		// added the latest log we need to subtract by 2 to find the previous
		prevLogTerm := 0 // Default if no previous logs
		if prevLogIndex >= 0 {
			prevLogTerm = s.Logs.logs[prevLogIndex].Term
		}

		appendEntriesReq := AppendEntriesRequest{
			Term:         s.currentTerm,
			LeaderID:     s.ID,
			PrevLogIndex: prevLogIndex,
			PrevLogTerm:  prevLogTerm,
			Entries:      entries,
			LeaderCommit: s.commitIndex,
		}

		// encodedData, err := Encode(appendEntriesReq)
		// if err != nil {
		// 	fmt.Println("Encoding error:", err)
		// 	return
		// }

		raftMessage := RaftMessage{
			Type:     AppendEntriesRPC,
			Data:     appendEntriesReq,
			SenderID: s.ID,
		}

		parts := strings.Split(peerIpPort, ":")

		peerIP := parts[0]
		peerPort, err := strconv.Atoi(parts[1])
		if err != nil {
			fmt.Printf("Error while parsing the port: %s\n", peerIpPort)
		}
		peer := Peer{
			ID:     peerID,
			IpAddr: peerIP,
			Port:   peerPort,
		}

		err = sendMessage(peer, raftMessage)
		if err != nil {
			fmt.Printf("❌ Failed to send log to peer %d (%s:%d): %v\n", peer.ID, peer.IpAddr, peer.Port, err)
		}
	}
}

// sendRaftMessage sends a Raft message to a peer identified by ID
func (s *Server) sendRaftMessage(peerID int, raftMessage RaftMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Get peer address from the node map
	peerIpPort, exists := s.NodeAddressMap[peerID]
	if !exists {
		return fmt.Errorf("unknown peer ID: %d", peerID)
	}

	// Parse peer address
	parts := strings.Split(peerIpPort, ":")
	if len(parts) != 2 {
		return fmt.Errorf("invalid peer address format: %s", peerIpPort)
	}

	peerIP := parts[0]
	peerPort, err := strconv.Atoi(parts[1])
	if err != nil {
		return fmt.Errorf("invalid port number: %s", parts[1])
	}

	// Create peer struct
	peer := Peer{
		ID:     peerID,
		IpAddr: peerIP,
		Port:   peerPort,
	}

	if err := sendMessage(peer, raftMessage); err != nil {
		return fmt.Errorf("failed to send message to peer %d (%s:%d): %w",
			peer.ID, peer.IpAddr, peer.Port, err)
	}

	return nil
}

///////////////////////////////////////////////////////////////////////////////

func (s *Server) getLastLogAndLastTerm() (int, int) {
	var lastLogIndex, lastLogTerm int
	n := len(s.Logs.logs)
	if n == 0 {
		return -1, -1
	}
	lastLogIndex = n - 1
	lastLogTerm = s.Logs.logs[n-1].Term
	return lastLogIndex, lastLogTerm
}

// Get retrieves a value for a given key and logs the operation
func (s *Server) Get(key string) ([]byte, error) {
	val, err := s.Store.Get(key)
	if err != nil {
		return nil, err
	}

	// Log the GET operation (optional, since GETs are read-only)
	logEntry := Log{Term: s.currentTerm, Command: fmt.Sprintf("GET %s", key)}
	s.AppendEntry(logEntry)

	return val, nil
}

// Set adds or updates a key-value pair and logs the operation
func (s *Server) Set(key string, val []byte) {
	// Log the SET operation
	logEntry := Log{Term: s.currentTerm, Command: fmt.Sprintf("SET %s %s", key, string(val))}
	s.AppendEntry(logEntry)
	s.Store.Set(key, val)

}

// Delete removes a key from the store and logs the operation
func (s *Server) Delete(key string) {
	// Log the DELETE operation
	logEntry := Log{Term: s.currentTerm, Command: fmt.Sprintf("DELETE %s", key)}
	s.AppendEntry(logEntry)

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

// Encode message to byte slice
func Encode(msg interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err := enc.Encode(msg)
	return buf.Bytes(), err
}
