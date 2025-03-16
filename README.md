# raft-inmemory-kv

My attempt at implementing RAFT based inmemorykey-value data store

Roadmap:

- [X] Implement a basic key-value server
- [X] Write unit tests for in-memory key-value server
- [X] Add support for REPL
- [] Implement logs
- [] Implement log replication
- [] Implement leader election
- [] Testing

Learnings

- In go you can keep class in internal directory
- If you want multiple executables you can keep it in cmd directory
- Root dir name doesn't matter you can keep it anything but while importing it inside
the executables say repl.go use import "raftkv/internal/raftkv
- internal packages cannot be imported in external packages via go get
-
