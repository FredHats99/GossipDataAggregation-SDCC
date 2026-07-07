package transport

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryingSenderRetriesUntilSuccess(t *testing.T) {
	sender := &recordingSender{failuresBeforeSuccess: 2}
	retrying, err := NewRetryingSender(sender, RetryConfig{
		MaxAttempts: 3,
		BaseBackoff: time.Millisecond,
		MaxBackoff:  time.Millisecond,
	})
	if err != nil {
		t.Fatalf("create retrying sender: %v", err)
	}
	retrying.sleep = func(context.Context, time.Duration) error { return nil }

	err = retrying.Send(context.Background(), "node2:7000", Envelope{})
	if err != nil {
		t.Fatalf("expected retry send to succeed, got %v", err)
	}
	if sender.calls != 3 {
		t.Fatalf("expected 3 send attempts, got %d", sender.calls)
	}
}

func TestRetryingSenderReturnsLastError(t *testing.T) {
	sender := &recordingSender{failuresBeforeSuccess: 10}
	retrying, err := NewRetryingSender(sender, RetryConfig{
		MaxAttempts: 2,
		BaseBackoff: time.Millisecond,
		MaxBackoff:  time.Millisecond,
	})
	if err != nil {
		t.Fatalf("create retrying sender: %v", err)
	}
	retrying.sleep = func(context.Context, time.Duration) error { return nil }

	err = retrying.Send(context.Background(), "node2:7000", Envelope{})
	if !errors.Is(err, errSendFailed) {
		t.Fatalf("expected send failure, got %v", err)
	}
	if sender.calls != 2 {
		t.Fatalf("expected 2 send attempts, got %d", sender.calls)
	}
}

func TestMessageGuardRejectsDuplicateAndStaleSequence(t *testing.T) {
	guard := NewMessageGuard(time.Minute)
	first := Envelope{From: "node1", Seq: 10}
	if err := guard.Accept(first); err != nil {
		t.Fatalf("first message rejected: %v", err)
	}
	if err := guard.Accept(first); !errors.Is(err, ErrDuplicateMessage) {
		t.Fatalf("expected duplicate rejection, got %v", err)
	}
	if err := guard.Accept(Envelope{From: "node1", Seq: 9}); !errors.Is(err, ErrStaleSequence) {
		t.Fatalf("expected stale sequence rejection, got %v", err)
	}
	if err := guard.Accept(Envelope{From: "node1", Seq: 11}); err != nil {
		t.Fatalf("higher sequence rejected: %v", err)
	}
}

func TestMessageGuardTTLDoesNotPermitSequenceRollback(t *testing.T) {
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	guard := NewMessageGuard(time.Second)
	guard.now = func() time.Time { return now }

	if err := guard.Accept(Envelope{From: "node1", Seq: 10}); err != nil {
		t.Fatalf("first message rejected: %v", err)
	}
	now = now.Add(2 * time.Second)
	if err := guard.Accept(Envelope{From: "node1", Seq: 10}); !errors.Is(err, ErrStaleSequence) {
		t.Fatalf("expected stale sequence after TTL expiry, got %v", err)
	}
}

func TestGuardedReceiverSkipsDuplicateMessages(t *testing.T) {
	receiver := &sliceReceiver{
		messages: []Envelope{
			{From: "node1", Seq: 1},
			{From: "node1", Seq: 1},
			{From: "node1", Seq: 2},
		},
	}
	guarded, err := NewGuardedReceiver(receiver, NewMessageGuard(time.Minute))
	if err != nil {
		t.Fatalf("create guarded receiver: %v", err)
	}

	first, err := guarded.Next(context.Background())
	if err != nil {
		t.Fatalf("first next failed: %v", err)
	}
	second, err := guarded.Next(context.Background())
	if err != nil {
		t.Fatalf("second next failed: %v", err)
	}
	if first.Seq != 1 || second.Seq != 2 {
		t.Fatalf("expected sequences 1 and 2, got %d and %d", first.Seq, second.Seq)
	}
}

var errSendFailed = errors.New("send failed")

type recordingSender struct {
	calls                 int
	failuresBeforeSuccess int
}

func (s *recordingSender) Send(context.Context, string, Envelope) error {
	s.calls++
	if s.calls <= s.failuresBeforeSuccess {
		return errSendFailed
	}
	return nil
}

type sliceReceiver struct {
	messages []Envelope
	index    int
}

func (r *sliceReceiver) Next(context.Context) (Envelope, error) {
	if r.index >= len(r.messages) {
		return Envelope{}, context.Canceled
	}
	message := r.messages[r.index]
	r.index++
	return message, nil
}

func (r *sliceReceiver) Close() error {
	return nil
}
