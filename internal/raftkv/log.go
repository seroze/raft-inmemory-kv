package raftkv

import "fmt"
import "encoding/gob"

func init() {
	gob.Register(Log{})
}

type Log struct {
	Term    int
	Command string
}


// String method to satisfy the fmt.Stringer interface
func (l Log) String() string {
	return fmt.Sprintf("Log{Term: %d, Command: %q}", l.Term, l.Command)
}



type Logs struct {
	logs []Log
}

func NewLogs() *Logs {
	return &Logs{
		logs: make([]Log, 0),
	}
}

func (logs *Logs) AppendLog(log Log) {
	fmt.Println("Appending log: ", log)
	// append this log
	logs.logs = append(logs.logs, log)
}
