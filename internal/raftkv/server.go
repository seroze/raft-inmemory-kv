package raftkv

import (
	"fmt"
	// "raftkv/internal/raftkv"
	"encoding/gob"
	"sync"
	"time"
	"net"
	"strings"
)

const (
	LOCAL_IPADDR = "127.0.0.1"
)

// Server represents a node in the Raft cluster
type Server struct {
	ID     int             // Unique server ID (1-5)
	IpAddr string 		   // IpAddress
	Port   int 			   // port 
	Logs   *Logs           // Log storage for RAFT replication
	Store  *Store          // The actual key-value store
	Peers  []Peer       // List of peer servers
	mu     sync.Mutex      // Mutex to prevent race conditions
	leader bool            // If true, this server is the leader
}

type Peer struct {
	ID int // id of the peer. (1-5)
	IpAddr string // ip address of peer 
	Port int // port 
}

// NewServer initializes a new Server instance
func NewServer(id int, ipAddr string, port int) *Server {
	server := &Server{
		ID:    id,
		IpAddr: LOCAL_IPADDR,
		Port:  port,
		Store: NewStore(),
		Logs:  NewLogs(),
		Peers: []Peer{}, // Empty list, will be populated later
	}

	if server.ID==1{
		//set node 1 as leader 
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

	// Create GOB decoder
	decoder := gob.NewDecoder(conn)
	var logEntry Log

	// Decode Log struct from received data
	err := decoder.Decode(&logEntry)
	if err != nil {
		fmt.Printf("❌ Failed to decode log entry: %v\n", err)
		return
	}

	// Process received log entry
	fmt.Printf("📩 Server %d received log entry: %+v\n", s.ID, logEntry)
	s.Logs.AppendLog(logEntry)
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
func (s *Server) AddPeer(peer Peer) {
	s.Peers = append(s.Peers, peer)
}

// SetLeader makes this server the leader
func (s *Server) SetLeader() {
	s.leader = true
	fmt.Printf("Server %d is now the leader\n", s.ID)
}

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
	for _, peer := range s.Peers {
		if peer.ID == s.ID {
			// Skip sending to self
			continue
		}

		err := SendRPCMessage(peer, logEntry) // Send entire Log struct
		if err != nil {
			fmt.Printf("❌ Failed to send log to peer %d (%s:%d): %v\n", peer.ID, peer.IpAddr, peer.Port, err)
		}
	}
}

// func (s *Server) AppendEntry(command string) {
// 	s.mu.Lock()
// 	defer s.mu.Unlock()

// 	// Append log entry
// 	logEntry := Log{Term: 1, Command: command}
// 	s.Logs.AppendLog(logEntry)

// 	// Leader should only append logs
// 	if !s.leader {
// 		fmt.Printf("Server %d is not the leader, skipping log replication\n", s.ID)
// 		return
// 	}

// 	// Replicate log entry to all peers
// 	for _, peer := range s.Peers {
// 		if peer.ID == s.ID {
// 			// Skip sending message to self
// 			continue
// 		}
// 		fmt.Printf("Sending log entry to peer %d at %s:%d\n", peer.ID, peer.IpAddr, peer.Port)

// 		// Capture and print errors
// 		err := SendRPCMessage(peer, command)
// 		if err != nil {
// 			fmt.Printf("Failed to send message to peer %d (%s:%d): %v\n", peer.ID, peer.IpAddr, peer.Port, err)
// 		}
// 	}
// }


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
