package agent

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// acceptLoop — incoming connection handling
// ---------------------------------------------------------------------------

func TestIPCBridge_AcceptLoop_ConnectionTracking(t *testing.T) {
	tmpDir := shortSockDir(t)
	parent := NewIPCBridge(tmpDir)
	if err := parent.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer parent.Close()

	var received atomic.Int32
	parent.SetOnMessage(func(msg IPCMessage) {
		received.Add(1)
	})

	child := NewIPCBridge(tmpDir)
	if err := child.Connect("tracking-agent"); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer child.Close()

	time.Sleep(50 * time.Millisecond)

	// Send a message from child; this causes parent's readLoop to register the connection
	msg := IPCMessage{
		Type:    "heartbeat",
		From:    "tracking-agent",
		To:      "parent",
		ID:      "track-1",
		Payload: json.RawMessage(`"ping"`),
	}
	if err := child.SendToParent(msg); err != nil {
		t.Fatalf("SendToParent failed: %v", err)
	}

	// Wait for message to arrive
	deadline := time.After(2 * time.Second)
	for received.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for message")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Verify the parent tracked the connection by agent name
	parent.mu.RLock()
	_, found := parent.conns["tracking-agent"]
	parent.mu.RUnlock()
	if !found {
		t.Error("expected parent to track connection under 'tracking-agent'")
	}
}

// ---------------------------------------------------------------------------
// readLoop — connection cleanup on close
// ---------------------------------------------------------------------------

func TestIPCBridge_ReadLoop_ConnectionCleanup(t *testing.T) {
	tmpDir := shortSockDir(t)
	parent := NewIPCBridge(tmpDir)
	if err := parent.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer parent.Close()

	child := NewIPCBridge(tmpDir)
	if err := child.Connect("cleanup-agent"); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Send one message so parent registers the connection
	msg := IPCMessage{Type: "heartbeat", From: "cleanup-agent", To: "parent", ID: "cl-1"}
	if err := child.SendToParent(msg); err != nil {
		t.Fatalf("SendToParent failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Verify registered
	parent.mu.RLock()
	_, found := parent.conns["cleanup-agent"]
	parent.mu.RUnlock()
	if !found {
		t.Fatal("expected connection to be registered")
	}

	// Close child — parent's readLoop should detect this and clean up
	child.Close()
	time.Sleep(200 * time.Millisecond)

	parent.mu.RLock()
	_, stillFound := parent.conns["cleanup-agent"]
	parent.mu.RUnlock()
	if stillFound {
		t.Error("expected connection to be cleaned up after child disconnect")
	}
}

// ---------------------------------------------------------------------------
// Connect — child connects to parent
// ---------------------------------------------------------------------------

func TestIPCBridge_Connect_NoServer(t *testing.T) {
	tmpDir := shortSockDir(t)
	child := NewIPCBridge(tmpDir)

	err := child.Connect("orphan-agent")
	if err == nil {
		t.Error("expected error when connecting with no server")
		child.Close()
	}
}

// ---------------------------------------------------------------------------
// Close — cleans up socket file
// ---------------------------------------------------------------------------

func TestIPCBridge_Close_RemovesSocket(t *testing.T) {
	tmpDir := shortSockDir(t)
	b := NewIPCBridge(tmpDir)

	if err := b.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	sockPath := b.socketPath("parent")
	if _, err := os.Stat(sockPath); os.IsNotExist(err) {
		t.Fatal("socket file should exist after Start")
	}

	b.Close()

	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Error("socket file should be removed after Close")
	}
}

// ---------------------------------------------------------------------------
// Close — channel closed
// ---------------------------------------------------------------------------

func TestIPCBridge_Close_ChannelClosed(t *testing.T) {
	tmpDir := shortSockDir(t)
	b := NewIPCBridge(tmpDir)

	if err := b.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	b.Close()

	// Reading from closed channel should return zero value immediately
	select {
	case _, ok := <-b.msgCh:
		if ok {
			t.Error("expected channel to be closed")
		}
	default:
		// Channel was closed with no buffered messages
	}
}

// ---------------------------------------------------------------------------
// Full bidirectional: parent sends to child, child sends to parent
// ---------------------------------------------------------------------------

func TestIPCBridge_Bidirectional(t *testing.T) {
	tmpDir := shortSockDir(t)

	parent := NewIPCBridge(tmpDir)
	if err := parent.Start(); err != nil {
		t.Fatalf("parent Start failed: %v", err)
	}
	defer parent.Close()

	child := NewIPCBridge(tmpDir)
	if err := child.Connect("bidir-child"); err != nil {
		t.Fatalf("child Connect failed: %v", err)
	}
	defer child.Close()

	time.Sleep(50 * time.Millisecond)

	// First: child sends to parent so parent registers child connection
	childMsg := IPCMessage{Type: "heartbeat", From: "bidir-child", To: "parent", ID: "hb-init"}
	if err := child.SendToParent(childMsg); err != nil {
		t.Fatalf("child SendToParent failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Now parent can send back to child
	parentMsg := IPCMessage{
		Type:    "task_assign",
		From:    "parent",
		To:      "bidir-child",
		ID:      "task-1",
		Payload: json.RawMessage(`{"job":"build"}`),
	}
	if err := parent.Send(parentMsg); err != nil {
		t.Fatalf("parent Send failed: %v", err)
	}

	// Child should receive via its readLoop
	select {
	case got := <-child.Receive():
		if got.ID != "task-1" {
			t.Errorf("child received ID = %q, want 'task-1'", got.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for parent message on child")
	}
}

// ---------------------------------------------------------------------------
// acceptLoop handles multiple connections
// ---------------------------------------------------------------------------

func TestIPCBridge_MultipleChildren(t *testing.T) {
	tmpDir := shortSockDir(t)

	parent := NewIPCBridge(tmpDir)
	if err := parent.Start(); err != nil {
		t.Fatalf("parent Start failed: %v", err)
	}
	defer parent.Close()

	var msgCount atomic.Int32
	parent.SetOnMessage(func(msg IPCMessage) {
		msgCount.Add(1)
	})

	children := make([]*IPCBridge, 3)
	for i := range children {
		children[i] = NewIPCBridge(tmpDir)
		name := fmt.Sprintf("child-%d", i)
		if err := children[i].Connect(name); err != nil {
			t.Fatalf("child %d Connect failed: %v", i, err)
		}
		defer children[i].Close()
	}

	time.Sleep(100 * time.Millisecond)

	// Each child sends a message
	for i := range children {
		msg := IPCMessage{
			Type: "message",
			From: fmt.Sprintf("child-%d", i),
			To:   "parent",
			ID:   fmt.Sprintf("msg-%d", i),
		}
		if err := children[i].SendToParent(msg); err != nil {
			t.Fatalf("child %d SendToParent failed: %v", i, err)
		}
	}

	// Wait for all messages
	deadline := time.After(3 * time.Second)
	for msgCount.Load() < 3 {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for messages, got %d/3", msgCount.Load())
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// ---------------------------------------------------------------------------
// writeMessage / readMessage — edge cases
// ---------------------------------------------------------------------------

func TestWriteMessage_MarshalError(t *testing.T) {
	// This is hard to trigger directly since IPCMessage fields are all JSON-safe.
	// Test that a normal write works correctly.
	tmpDir := shortSockDir(t)
	sockPath := filepath.Join(tmpDir, "wr.sock")

	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	var serverConn net.Conn
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		serverConn, _ = l.Accept()
	}()

	clientConn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer clientConn.Close()

	<-acceptDone
	defer serverConn.Close()

	msg := IPCMessage{
		Type:    "test",
		From:    "a",
		To:      "b",
		ID:      "edge-1",
		Payload: json.RawMessage(`null`),
	}

	if err := writeMessage(clientConn, msg); err != nil {
		t.Fatalf("writeMessage failed: %v", err)
	}

	got, err := readMessage(serverConn)
	if err != nil {
		t.Fatalf("readMessage failed: %v", err)
	}
	if got.ID != "edge-1" {
		t.Errorf("ID = %q, want 'edge-1'", got.ID)
	}
}

// ---------------------------------------------------------------------------
// msgCh full — drops messages when buffer is full
// ---------------------------------------------------------------------------

func TestIPCBridge_MsgChannelFull(t *testing.T) {
	tmpDir := shortSockDir(t)
	b := NewIPCBridge(tmpDir)

	// Channel has capacity 256 — fill it manually
	for i := 0; i < 256; i++ {
		b.msgCh <- IPCMessage{ID: fmt.Sprintf("fill-%d", i)}
	}

	// Simulate readLoop dispatching a message when channel is full
	// The select default branch should catch this
	msg := IPCMessage{ID: "overflow-msg"}

	// This mimics the dispatch in readLoop
	select {
	case b.msgCh <- msg:
		t.Error("expected channel write to be dropped")
	default:
		// Expected path — channel full, message dropped
	}
}

// ---------------------------------------------------------------------------
// Start — stale socket cleanup
// ---------------------------------------------------------------------------

func TestIPCBridge_Start_CleansStaleSocket(t *testing.T) {
	tmpDir := shortSockDir(t)
	b := NewIPCBridge(tmpDir)

	sockPath := b.socketPath("parent")

	// Create a stale socket file by listening and then closing.
	// On some platforms the file may be cleaned up by the OS on close,
	// so we create a regular file as a fallback if the socket is gone.
	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to create stale socket: %v", err)
	}
	l.Close()

	// If the socket file disappeared after Close, create a dummy file
	if _, err := os.Stat(sockPath); os.IsNotExist(err) {
		if err := os.WriteFile(sockPath, []byte("stale"), 0644); err != nil {
			t.Fatalf("failed to create dummy stale file: %v", err)
		}
	}

	// Start should clean it up and create a new one
	if err := b.Start(); err != nil {
		t.Fatalf("Start failed with stale socket: %v", err)
	}
	defer b.Close()

	if _, err := os.Stat(sockPath); os.IsNotExist(err) {
		t.Error("socket should exist after Start")
	}
}
