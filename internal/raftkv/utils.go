package raftkv

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	TOTAL_SERVERS = 5 //for now let's hard code
)

// LoadServerConfig reads a config file and returns a map of ServerID -> Port
func LoadServerConfig(configFile string) (map[int]string, error) {
	file, err := os.Open(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file: %v", err)
	}
	defer file.Close()

	serverMap := make(map[int]string)
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") { // Ignore empty lines and comments
			continue
		}

		parts := strings.Split(line, "=")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid config format: %s", line)
		}

		ipPort := strings.TrimSpace(parts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid port number in config: %s", parts[1])
		}

		serverID, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil {
			return nil, fmt.Errorf("invalid server ID in config: %s", parts[0])
		}

		serverMap[serverID] = ipPort
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading config file: %v", err)
	}

	return serverMap, nil
}
