package transport

import (
	"context"
	"errors"
	"sync"
	"time"
)

type RetryConfig struct {
	MaxAttempts int
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
}

func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts: 3,
		BaseBackoff: 100 * time.Millisecond,
		MaxBackoff:  1 * time.Second,
	}
}

type RetryingSender struct {
	sender Sender
	config RetryConfig
	sleep  func(context.Context, time.Duration) error
}

func NewRetryingSender(sender Sender, config RetryConfig) (*RetryingSender, error) {
	if sender == nil {
		return nil, ErrNilSender
	}
	if config.MaxAttempts <= 0 {
		config.MaxAttempts = DefaultRetryConfig().MaxAttempts
	}
	if config.BaseBackoff <= 0 {
		config.BaseBackoff = DefaultRetryConfig().BaseBackoff
	}
	if config.MaxBackoff <= 0 {
		config.MaxBackoff = DefaultRetryConfig().MaxBackoff
	}
	return &RetryingSender{
		sender: sender,
		config: config,
		sleep:  sleepContext,
	}, nil
}

func (s *RetryingSender) Send(ctx context.Context, peer string, message Envelope) error {
	var lastErr error
	for attempt := 1; attempt <= s.config.MaxAttempts; attempt++ {
		if err := s.sender.Send(ctx, peer, message); err != nil {
			lastErr = err
		} else {
			return nil
		}

		if attempt == s.config.MaxAttempts {
			break
		}
		if err := s.sleep(ctx, backoffForAttempt(attempt, s.config)); err != nil {
			return err
		}
	}
	return lastErr
}

func backoffForAttempt(attempt int, config RetryConfig) time.Duration {
	backoff := config.BaseBackoff
	for i := 1; i < attempt; i++ {
		backoff *= 2
		if backoff >= config.MaxBackoff {
			return config.MaxBackoff
		}
	}
	if backoff > config.MaxBackoff {
		return config.MaxBackoff
	}
	return backoff
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type MessageGuard struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time
	seen    map[messageKey]time.Time
	highSeq map[string]uint64
}

type messageKey struct {
	from string
	seq  uint64
}

func NewMessageGuard(ttl time.Duration) *MessageGuard {
	if ttl <= 0 {
		ttl = time.Minute
	}
	return &MessageGuard{
		ttl:     ttl,
		now:     time.Now,
		seen:    make(map[messageKey]time.Time),
		highSeq: make(map[string]uint64),
	}
}

func (g *MessageGuard) Accept(message Envelope) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := g.now().UTC()
	g.prune(now)

	key := messageKey{from: message.From, seq: message.Seq}
	if expiresAt, ok := g.seen[key]; ok && now.Before(expiresAt) {
		return ErrDuplicateMessage
	}

	if high, ok := g.highSeq[message.From]; ok && message.Seq <= high {
		return ErrStaleSequence
	}

	g.seen[key] = now.Add(g.ttl)
	g.highSeq[message.From] = message.Seq
	return nil
}

func (g *MessageGuard) prune(now time.Time) {
	for key, expiresAt := range g.seen {
		if !now.Before(expiresAt) {
			delete(g.seen, key)
		}
	}
}

type GuardedReceiver struct {
	receiver Receiver
	guard    *MessageGuard
}

func NewGuardedReceiver(receiver Receiver, guard *MessageGuard) (*GuardedReceiver, error) {
	if receiver == nil {
		return nil, ErrNilReceiver
	}
	if guard == nil {
		guard = NewMessageGuard(time.Minute)
	}
	return &GuardedReceiver{
		receiver: receiver,
		guard:    guard,
	}, nil
}

func (r *GuardedReceiver) Next(ctx context.Context) (Envelope, error) {
	for {
		message, err := r.receiver.Next(ctx)
		if err != nil {
			return Envelope{}, err
		}
		if err := r.guard.Accept(message); err != nil {
			if errors.Is(err, ErrDuplicateMessage) || errors.Is(err, ErrStaleSequence) {
				continue
			}
			return Envelope{}, err
		}
		return message, nil
	}
}

func (r *GuardedReceiver) Close() error {
	return r.receiver.Close()
}
