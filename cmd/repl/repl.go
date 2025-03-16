package main

import (
	"bufio"
	"fmt"
	"os"
	"raftkv/internal/raftkv"
	"strings"
	"strconv"
	// "raft-inmemory-kv/internal/raftkv" // Adjust the import path based on your module name
)

func main() {

	// Check if an argument was provided
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go <server_id>")
		return
	}

	// Convert the argument to an integer
	serverID, err := strconv.Atoi(os.Args[1])
	if err != nil || serverID < 1 || serverID > 5 {
		fmt.Println("Error: Server ID must be a number between 1 and 5")
		return
	}

	configFile := "servers.config"
	// fmt.Println(raftkv.TOTAL_SERVERS)
	serverMap, err := raftkv.LoadServerConfig(configFile)
	if err != nil {
		fmt.Println("Error loading server config:", err)
		return
	}

	fmt.Println("Loaded Server Config:", serverMap)

	// store := raftkv.NewStore() // Initialize the store
	server := raftkv.NewServer(serverID, raftkv.LOCAL_IPADDR, serverMap[serverID]) 

	for id,port := range serverMap{
		server.AddPeer(raftkv.Peer{ID: id, IpAddr: "127.0.0.1", Port: port})
	}

	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Welcome to RaftKV REPL server: ", serverID)
	fmt.Println("Commands: SET <key> <value>, GET <key>, DELETE <key>, EXIT")

	for {
		fmt.Print("> ")
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("Error reading input:", err)
			continue
		}

		input = strings.TrimSpace(input) // will trim spaces at start and end
		// parts := strings.SplitN(input, " ", 3) // Split input into max 3 parts
		parts := strings.Fields(input) // Splits by any number of spaces

		if len(parts) == 0 {
			continue
		}
		if len(parts) > 3 {
			fmt.Println("Error incorrect input: it has more than 3 spaces")
		}

		command := strings.ToUpper(parts[0])

		switch command {
		case "SET":
			if len(parts) < 3 {
				fmt.Println("Usage: SET <key> <value>")
				continue
			}
			key := parts[1]
			value := []byte(parts[2])
			server.Set(key, value)
			fmt.Println("OK")

		case "GET":
			if len(parts) < 2 {
				fmt.Println("Usage: GET <key>")
				continue
			}
			key := parts[1]
			value, err := server.Get(key)
			if err != nil {
				fmt.Println("Error:", err)
			} else {
				fmt.Println("Value:", string(value))
			}

		case "DELETE":
			if len(parts) < 2 {
				fmt.Println("Usage: DELETE <key>")
				continue
			}
			key := parts[1]
			server.Delete(key)
			fmt.Println("Deleted", key)

		case "EXIT":
			fmt.Println("Exiting REPL")
			return

		default:
			fmt.Println("Unknown command. Available commands: SET, GET, DELETE, EXIT")
		}
	}
}
