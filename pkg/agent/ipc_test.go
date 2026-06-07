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

// shortSockDir creates a temp directory with a short path for Unix sockets.
// macOS has a 104-byte limit on socket paths, so t.TempDir() paths are too long.
func shortSockDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("ipc_%d", time.Now().UnixNano()))
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// ---------------------------------------------------------------------------
// NewIPCBridge constructor
// ---------------------------------------------------------------------------

func TestNewIPCBridge(t *testing.T) {
	tmpDir := shortSockDir(t)
	b := NewIPCBridge(tmpDir)

	if b == nil {
		t.Fatal("NewIPCBridge returned nil")
	}
	if b.socketDir != tmpDir {
		t.Errorf("socketDir = %q, want %q", b.socketDir, tmpDir)
	}
	if b.conns == nil {
		t.Error("conns map should be initialized")
	}
	if b.msgCh == nil {
		t.Error("msgCh should be initialized")
	}
}

// ---------------------------------------------------------------------------
// socketPath formatting
// ---------------------------------------------------------------------------

func TestIPCBridge_SocketPath(t *testing.T) {
	tmpDir := shortSockDir(t)
	b := NewIPCBridge(tmpDir)

	tests := []struct {
		agent string
		want  string
	}{
		{"parent", filepath.Join(tmpDir, "iroha-parent.sock")},
		{"worker-1", filepath.Join(tmpDir, "iroha-worker-1.sock")},
		{"", filepath.Join(tmpDir, "iroha-.sock")},
	}

	for _, tt := range tests {
		got := b.socketPath(tt.agent)
		if got != tt.want {
			t.Errorf("socketPath(%q) = %q, want %q", tt.agent, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Start / Close lifecycle
// ---------------------------------------------------------------------------

func TestIPCBridge_StartClose(t *testing.T) {
	tmpDir := shortSockDir(t)
	b := NewIPCBridge(tmpDir)

	if err := b.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Verify socket file was created
	sockPath := b.socketPath("parent")
	if _, err := os.Stat(sockPath); os.IsNotExist(err) {
		t.Error("socket file should exist after Start")
	}

	// Close should clean up
	b.Close()

	if !b.closed.Load() {
		t.Error("closed flag should be true after Close")
	}
}

// ---------------------------------------------------------------------------
// readMessage / writeMessage round-trip via paired connections
// ---------------------------------------------------------------------------

func TestIPCBridge_ReadWriteMessage(t *testing.T) {
	tmpDir := shortSockDir(t)
	sockPath := filepath.Join(tmpDir, "t.sock")

	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer l.Close()

	var serverConn net.Conn
	var acceptErr error
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		serverConn, acceptErr = l.Accept()
	}()

	clientConn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer clientConn.Close()

	<-acceptDone
	if acceptErr != nil {
		t.Fatalf("accept failed: %v", acceptErr)
	}
	defer serverConn.Close()

	original := IPCMessage{
		Type:    "task_assign",
		From:    "parent",
		To:      "child",
		ID:      "test-msg-001",
		Payload: json.RawMessage(`{"task":"build"}`),
	}

	if err := writeMessage(clientConn, original); err != nil {
		t.Fatalf("writeMessage failed: %v", err)
	}

	received, err := readMessage(serverConn)
	if err != nil {
		t.Fatalf("readMessage failed: %v", err)
	}

	if received.Type != original.Type {
		t.Errorf("Type = %q, want %q", received.Type, original.Type)
	}
	if received.From != original.From {
		t.Errorf("From = %q, want %q", received.From, original.From)
	}
	if received.To != original.To {
		t.Errorf("To = %q, want %q", received.To, original.To)
	}
	if received.ID != original.ID {
		t.Errorf("ID = %q, want %q", received.ID, original.ID)
	}
}

func TestIPCBridge_ReadWriteMultipleMessages(t *testing.T) {
	tmpDir := shortSockDir(t)
	sockPath := filepath.Join(tmpDir, "m.sock")

	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
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
		t.Fatalf("failed to dial: %v", err)
	}
	defer clientConn.Close()

	<-acceptDone
	defer serverConn.Close()

	msgs := []IPCMessage{
		{Type: "heartbeat", From: "child", To: "parent", ID: "hb-1"},
		{Type: "message", From: "child", To: "parent", ID: "msg-1", Payload: json.RawMessage(`"hello"`)},
		{Type: "task_complete", From: "child", To: "parent", ID: "tc-1"},
	}

	for _, m := range msgs {
		if err := writeMessage(clientConn, m); err != nil {
			t.Fatalf("writeMessage(%s) failed: %v", m.ID, err)
		}
	}

	for i, expected := range msgs {
		got, err := readMessage(serverConn)
		if err != nil {
			t.Fatalf("readMessage(%d) failed: %v", i, err)
		}
		if got.ID != expected.ID {
			t.Errorf("msg %d: ID = %q, want %q", i, got.ID, expected.ID)
		}
		if got.Type != expected.Type {
			t.Errorf("msg %d: Type = %q, want %q", i, got.Type, expected.Type)
		}
	}
}

// ---------------------------------------------------------------------------
// Send to registered connection
// ---------------------------------------------------------------------------

func TestIPCBridge_Send(t *testing.T) {
	tmpDir := shortSockDir(t)
	b := NewIPCBridge(tmpDir)

	sockPath := filepath.Join(tmpDir, "p.sock")
	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen failed: %v", err)
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
		t.Fatalf("dial failed: %v", err)
	}
	defer clientConn.Close()

	<-acceptDone
	defer serverConn.Close()

	b.mu.Lock()
	b.conns["target-agent"] = clientConn
	b.mu.Unlock()

	msg := IPCMessage{
		Type:    "message",
		From:    "sender",
		To:      "target-agent",
		ID:      "send-test",
		Payload: json.RawMessage(`"payload"`),
	}

	if err := b.Send(msg); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	received, err := readMessage(serverConn)
	if err != nil {
		t.Fatalf("readMessage failed: %v", err)
	}
	if received.ID != "send-test" {
		t.Errorf("received ID = %q, want 'send-test'", received.ID)
	}
}

func TestIPCBridge_Send_NoConnection(t *testing.T) {
	tmpDir := shortSockDir(t)
	b := NewIPCBridge(tmpDir)

	err := b.Send(IPCMessage{To: "nonexistent", ID: "x"})
	if err == nil {
		t.Error("expected error when sending to nonexistent agent")
	}
}

// ---------------------------------------------------------------------------
// SendToParent
// ---------------------------------------------------------------------------

func TestIPCBridge_SendToParent(t *testing.T) {
	tmpDir := shortSockDir(t)
	b := NewIPCBridge(tmpDir)

	sockPath := filepath.Join(tmpDir, "pr.sock")
	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen failed: %v", err)
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
		t.Fatalf("dial failed: %v", err)
	}
	defer clientConn.Close()

	<-acceptDone
	defer serverConn.Close()

	b.mu.Lock()
	b.conns["child-agent"] = clientConn
	b.mu.Unlock()

	msg := IPCMessage{
		Type:    "message",
		From:    "child-agent",
		To:      "parent",
		ID:      "to-parent-1",
		Payload: json.RawMessage(`"data"`),
	}

	if err := b.SendToParent(msg); err != nil {
		t.Fatalf("SendToParent failed: %v", err)
	}

	received, err := readMessage(serverConn)
	if err != nil {
		t.Fatalf("readMessage failed: %v", err)
	}
	if received.ID != "to-parent-1" {
		t.Errorf("received ID = %q, want 'to-parent-1'", received.ID)
	}
}

func TestIPCBridge_SendToParent_Fallback(t *testing.T) {
	tmpDir := shortSockDir(t)
	b := NewIPCBridge(tmpDir)

	sockPath := filepath.Join(tmpDir, "fb.sock")
	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen failed: %v", err)
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
		t.Fatalf("dial failed: %v", err)
	}
	defer clientConn.Close()

	<-acceptDone
	defer serverConn.Close()

	// Register with a different key than msg.From, so SendToParent falls back to "any available"
	b.mu.Lock()
	b.conns["some-other-key"] = clientConn
	b.mu.Unlock()

	msg := IPCMessage{
		Type: "message",
		From: "child",
		To:   "parent",
		ID:   "fallback-test",
	}

	if err := b.SendToParent(msg); err != nil {
		t.Fatalf("SendToParent fallback failed: %v", err)
	}

	received, err := readMessage(serverConn)
	if err != nil {
		t.Fatalf("readMessage failed: %v", err)
	}
	if received.ID != "fallback-test" {
		t.Errorf("received ID = %q, want 'fallback-test'", received.ID)
	}
}

func TestIPCBridge_SendToParent_NoConnection(t *testing.T) {
	tmpDir := shortSockDir(t)
	b := NewIPCBridge(tmpDir)

	err := b.SendToParent(IPCMessage{From: "orphan", ID: "x"})
	if err == nil {
		t.Error("expected error when no connection to parent")
	}
}

// ---------------------------------------------------------------------------
// Receive channel delivery
// ---------------------------------------------------------------------------

func TestIPCBridge_Receive(t *testing.T) {
	tmpDir := shortSockDir(t)
	b := NewIPCBridge(tmpDir)

	ch := b.Receive()
	if ch == nil {
		t.Fatal("Receive returned nil channel")
	}
}

// ---------------------------------------------------------------------------
// SetOnMessage callback
// ---------------------------------------------------------------------------

func TestIPCBridge_SetOnMessage(t *testing.T) {
	tmpDir := shortSockDir(t)
	b := NewIPCBridge(tmpDir)

	var called atomic.Int32
	b.SetOnMessage(func(msg IPCMessage) {
		called.Add(1)
	})

	b.mu.RLock()
	handler := b.onMessage
	b.mu.RUnlock()

	if handler == nil {
		t.Fatal("onMessage should be set")
	}

	handler(IPCMessage{ID: "cb-test"})

	if called.Load() != 1 {
		t.Errorf("expected callback to be called once, got %d", called.Load())
	}
}

// ---------------------------------------------------------------------------
// Full integration: Start, Connect, Send, Receive, Close
// ---------------------------------------------------------------------------

func TestIPCBridge_Integration_SendReceive(t *testing.T) {
	tmpDir := shortSockDir(t)

	parent := NewIPCBridge(tmpDir)
	if err := parent.Start(); err != nil {
		t.Fatalf("parent Start failed: %v", err)
	}
	defer parent.Close()

	received := make(chan IPCMessage, 10)
	parent.SetOnMessage(func(msg IPCMessage) {
		received <- msg
	})

	child := NewIPCBridge(tmpDir)
	if err := child.Connect("worker-1"); err != nil {
		t.Fatalf("child Connect failed: %v", err)
	}
	defer child.Close()

	// Give the accept loop time to register the connection
	time.Sleep(50 * time.Millisecond)

	msg := IPCMessage{
		Type:    "task_complete",
		From:    "worker-1",
		To:      "parent",
		ID:      "integration-1",
		Payload: json.RawMessage(`{"status":"done"}`),
	}

	if err := child.SendToParent(msg); err != nil {
		t.Fatalf("child SendToParent failed: %v", err)
	}

	select {
	case got := <-received:
		if got.ID != "integration-1" {
			t.Errorf("received ID = %q, want 'integration-1'", got.ID)
		}
		if got.Type != "task_complete" {
			t.Errorf("received Type = %q, want 'task_complete'", got.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for message on parent")
	}
}

// ---------------------------------------------------------------------------
// readMessage edge cases
// ---------------------------------------------------------------------------

func TestReadMessage_ConnectionClosed(t *testing.T) {
	tmpDir := shortSockDir(t)
	sockPath := filepath.Join(tmpDir, "cl.sock")

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

	<-acceptDone

	// Close client immediately
	clientConn.Close()

	// Reading from server should get an error
	_, err = readMessage(serverConn)
	if err == nil {
		t.Error("expected error reading from closed connection")
	}
	_ = serverConn
}

func TestReadMessage_TooLarge(t *testing.T) {
	tmpDir := shortSockDir(t)
	sockPath := filepath.Join(tmpDir, "lg.sock")

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

	// Write a 4-byte length prefix claiming 16MB > 10MB limit
	lenBuf := make([]byte, 4)
	lenBuf[0] = 0x01 // 0x01000000 = 16777216 = 16MB
	lenBuf[1] = 0x00
	lenBuf[2] = 0x00
	lenBuf[3] = 0x00
	if _, err := clientConn.Write(lenBuf); err != nil {
		t.Fatal(err)
	}

	_, err = readMessage(serverConn)
	if err == nil {
		t.Error("expected error for oversized message")
	}
}
