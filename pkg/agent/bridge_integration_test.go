package agent

import (
	"sync/atomic"
	"testing"
	"time"
)

// ─── ConfirmationBridge Integration Tests ─────────────────────────────────────

func TestIntegration_ConfirmationBridge_RoundTrip(t *testing.T) {
	b := &ConfirmationBridge{
		PromptChan:   make(chan string, 1),
		ResponseChan: make(chan string, 1),
		CancelChan:   make(chan struct{}),
	}

	// Simulate agent sending a prompt and waiting for response
	promptReceived := make(chan string, 1)
	go func() {
		// Agent sends prompt
		b.PromptChan <- "Allow shell_run: rm -rf /tmp/test?"
		// Agent waits for response
		resp := <-b.ResponseChan
		promptReceived <- resp
	}()

	// Simulate TUI reading prompt and sending response
	time.Sleep(50 * time.Millisecond) // let goroutine start
	prompt := <-b.PromptChan
	if prompt != "Allow shell_run: rm -rf /tmp/test?" {
		t.Errorf("unexpected prompt: %q", prompt)
	}

	// TUI sends response
	b.ResponseChan <- "y"

	// Verify agent received the response
	select {
	case resp := <-promptReceived:
		if resp != "y" {
			t.Errorf("expected response 'y', got %q", resp)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for round-trip response")
	}
}

func TestIntegration_ConfirmationBridge_Reset(t *testing.T) {
	b := &ConfirmationBridge{
		PromptChan:   make(chan string, 1),
		ResponseChan: make(chan string, 1),
		CancelChan:   make(chan struct{}),
	}

	// Put stale data in channels
	b.PromptChan <- "stale prompt"
	b.ResponseChan <- "stale response"

	// Reset should drain both channels
	b.Reset()

	// Verify channels are empty
	if len(b.PromptChan) != 0 {
		t.Error("PromptChan should be empty after Reset")
	}
	if len(b.ResponseChan) != 0 {
		t.Error("ResponseChan should be empty after Reset")
	}

	// Verify new CancelChan is fresh (not closed)
	select {
	case <-b.CancelChan:
		t.Error("CancelChan should not be closed after Reset")
	default:
		// expected
	}
}

func TestIntegration_ConfirmationBridge_Cancel(t *testing.T) {
	b := &ConfirmationBridge{
		PromptChan:   make(chan string, 1),
		ResponseChan: make(chan string, 1),
		CancelChan:   make(chan struct{}),
	}

	// Cancel the bridge
	b.Cancel()

	// CancelChanRead should return a closed channel
	cancelCh := b.CancelChanRead()
	select {
	case <-cancelCh:
		// expected: channel is closed
	default:
		t.Error("CancelChanRead should return closed channel after Cancel()")
	}
}

func TestIntegration_ConfirmationBridge_CancelWhileSending(t *testing.T) {
	b := &ConfirmationBridge{
		PromptChan:   make(chan string, 1),
		ResponseChan: make(chan string, 1),
		CancelChan:   make(chan struct{}),
	}

	// Fill the PromptChan buffer so the goroutine blocks
	b.PromptChan <- "blocking message"

	cancelled := make(chan struct{})
	go func() {
		// Try to send prompt - this should block since buffer is full
		select {
		case b.PromptChan <- "test prompt":
		case <-b.CancelChanRead():
			close(cancelled)
		}
	}()

	// Give goroutine time to start and block
	time.Sleep(100 * time.Millisecond)

	// Cancel - should unblock the goroutine
	b.Cancel()

	select {
	case <-cancelled:
		// expected
	case <-time.After(2 * time.Second):
		t.Error("expected cancellation to unblock the sender")
	}
}

func TestIntegration_ConfirmationBridge_MultipleResetCycles(t *testing.T) {
	b := &ConfirmationBridge{
		PromptChan:   make(chan string, 1),
		ResponseChan: make(chan string, 1),
		CancelChan:   make(chan struct{}),
	}

	for i := 0; i < 5; i++ {
		b.Reset()

		// Send and receive in each cycle
		done := make(chan struct{})
		go func() {
			b.PromptChan <- "prompt"
			done <- struct{}{}
		}()

		<-b.PromptChan
		b.ResponseChan <- "y"
		<-done
	}
}

// ─── ToolStatusBridge Integration Tests ───────────────────────────────────────

func TestIntegration_ToolStatusBridge_SendAndReceive(t *testing.T) {
	tb := &ToolStatusBridge{
		StatusChan: make(chan ToolStatus, 100),
	}

	status := ToolStatus{
		Name:    "shell_run",
		Args:    map[string]any{"command": "echo hello"},
		Running: true,
	}

	tb.Send(status)

	// Read from StatusChan
	select {
	case received := <-tb.StatusChan:
		if received.Name != "shell_run" {
			t.Errorf("expected Name 'shell_run', got %q", received.Name)
		}
		if received.Running != true {
			t.Error("expected Running=true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for status on StatusChan")
	}
}

func TestIntegration_ToolStatusBridge_MultipleSends(t *testing.T) {
	tb := &ToolStatusBridge{
		StatusChan: make(chan ToolStatus, 100),
	}

	// Send multiple statuses
	statuses := []ToolStatus{
		{Name: "file_read", Running: true},
		{Name: "file_read", Running: false, Success: true},
		{Name: "shell_run", Running: true},
		{Name: "shell_run", Running: false, Error: nil},
	}

	for _, s := range statuses {
		tb.Send(s)
	}

	// Read all and verify order
	time.Sleep(200 * time.Millisecond) // wait for drain goroutine
	for i, expected := range statuses {
		select {
		case received := <-tb.StatusChan:
			if received.Name != expected.Name {
				t.Errorf("status %d: expected Name %q, got %q", i, expected.Name, received.Name)
			}
			if received.Running != expected.Running {
				t.Errorf("status %d: expected Running=%v, got Running=%v", i, expected.Running, received.Running)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout waiting for status %d", i)
		}
	}
}

func TestIntegration_ToolStatusBridge_DrainMechanics(t *testing.T) {
	tb := &ToolStatusBridge{
		StatusChan: make(chan ToolStatus, 100),
	}

	// Send 5 statuses rapidly
	for i := 0; i < 5; i++ {
		tb.Send(ToolStatus{
			Name:    "tool",
			Args:    map[string]any{"index": i},
			Running: true,
		})
	}

	// Wait for all 5 to arrive
	time.Sleep(200 * time.Millisecond)

	count := 0
	for {
		select {
		case <-tb.StatusChan:
			count++
		default:
			goto done
		}
	}
done:

	if count != 5 {
		t.Errorf("expected 5 statuses drained, got %d", count)
	}

	// Verify drain goroutine stopped (active flag should be false)
	tb.mu.Lock()
	active := tb.active
	tb.mu.Unlock()
	if active {
		t.Error("expected drain goroutine to be inactive after queue emptied")
	}
}

func TestIntegration_ToolStatusBridge_EmptyQueue(t *testing.T) {
	tb := &ToolStatusBridge{
		StatusChan: make(chan ToolStatus, 100),
	}

	// Send one status, wait for drain
	tb.Send(ToolStatus{Name: "test", Running: true})
	time.Sleep(100 * time.Millisecond)

	// Read the status
	<-tb.StatusChan

	// Now the drain goroutine should have exited
	tb.mu.Lock()
	active := tb.active
	tb.mu.Unlock()

	if active {
		t.Error("expected drain to stop after queue is empty")
	}

	// Sending again should restart the drain
	tb.Send(ToolStatus{Name: "test2", Running: false})
	time.Sleep(100 * time.Millisecond)

	select {
	case s := <-tb.StatusChan:
		if s.Name != "test2" {
			t.Errorf("expected 'test2', got %q", s.Name)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for second send")
	}
}

// ─── Concurrent access safety ────────────────────────────────────────────────

func TestIntegration_ToolStatusBridge_ConcurrentSends(t *testing.T) {
	tb := &ToolStatusBridge{
		StatusChan: make(chan ToolStatus, 1000),
	}

	var sent atomic.Int32
	for i := 0; i < 100; i++ {
		go func(idx int) {
			tb.Send(ToolStatus{
				Name:    "concurrent-tool",
				Args:    map[string]any{"worker": idx},
				Running: true,
			})
			sent.Add(1)
		}(i)
	}

	// Wait for all sends
	time.Sleep(500 * time.Millisecond)

	// Count received
	received := 0
	for {
		select {
		case <-tb.StatusChan:
			received++
		default:
			goto done2
		}
	}
done2:

	if int32(received) != sent.Load() {
		t.Errorf("expected %d received, got %d", sent.Load(), received)
	}
}

// ─── Global Bridge and ToolBridge tests ────────────────────────────────────────

func TestIntegration_GlobalBridgeReset(t *testing.T) {
	// Test that the global Bridge can be reset safely
	Bridge.Reset()

	// Verify channels are clean
	if len(Bridge.PromptChan) != 0 {
		t.Error("global PromptChan should be empty after Reset")
	}
	if len(Bridge.ResponseChan) != 0 {
		t.Error("global ResponseChan should be empty after Reset")
	}
}

func TestIntegration_GlobalToolBridgeSend(t *testing.T) {
	// Drain any pre-existing items from the global ToolBridge
	for {
		select {
		case <-ToolBridge.StatusChan:
		default:
			goto drained
		}
	}
drained:

	ToolBridge.Send(ToolStatus{
		Name:    "test-tool",
		Running: true,
	})

	select {
	case s := <-ToolBridge.StatusChan:
		if s.Name != "test-tool" {
			t.Errorf("expected 'test-tool', got %q", s.Name)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout receiving from global ToolBridge")
	}
}
