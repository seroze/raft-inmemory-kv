package raftkv

import "testing"

func TestNewLogs(t *testing.T) {
	logs := NewLogs()
	if logs == nil {
		t.Fatal("NewLogs() return nil")
	}
}

func TestAppendLog(t *testing.T) {
	logs := NewLogs()

	var log Log = Log{Term: 1, Command: "SET CAT MEOW"}
	logs.AppendLog(log)
	//check if logs size has increased by 1

	if len(logs.logs) != 1 {
		t.Errorf("Expected %d, got %d", 1, len(logs.logs))
	}
}
