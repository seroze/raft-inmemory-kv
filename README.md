# raft-inmemory-kv

My attempt at implementing RAFT based inmemorykey-value data store

Roadmap:

- [X] Implement a basic key-value server
- [X] Write unit tests for in-memory key-value server
- [X] Add support for REPL
- [X] Implement logs
- [X] Implement naive log replication
- [X] Test naive replication
- [] Implement AppendEntries and AppendEntriesResponse RPC
- [] Test AppendEntries and AppendEntriesResponse RPC
- [] Implement leader election via VoteRequest and VoteResponse RPC
- [] Test leader election

Issues:

- replica's are not handling get's, i know set's shouldn't be allowed
- how to make the rpc framework generic so that it can handle all kinds of RPCs
-

Learnings

- In go you can keep main logic in internal directory
- If you want multiple executables you can keep it in cmd directory and have seperate package, because go doesn't allow two main methods inside same package, they act as entry point
- Root dir name doesn't matter you can keep it anything but while importing it inside
the executables say repl.go use import "raftkv/internal/raftkv
- internal packages cannot be imported in external packages via go get
- if classess are in same package you can call one class objects from other

Resources:
- https://raft.github.io
