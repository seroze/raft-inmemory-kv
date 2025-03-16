package main

import (
	"bufio"
	"fmt"
	"os"
	"raftkv/internal/raftkv"
	"strings"
	// "raft-inmemory-kv/internal/raftkv" // Adjust the import path based on your module name
)

func main() {
	store := raftkv.NewStore() // Initialize the store
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Welcome to RaftKV REPL")
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
			store.Set(key, value)
			fmt.Println("OK")

		case "GET":
			if len(parts) < 2 {
				fmt.Println("Usage: GET <key>")
				continue
			}
			key := parts[1]
			value, err := store.Get(key)
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
			store.Delete(key)
			fmt.Println("Deleted", key)

		case "EXIT":
			fmt.Println("Exiting REPL")
			return

		default:
			fmt.Println("Unknown command. Available commands: SET, GET, DELETE, EXIT")
		}
	}
}
