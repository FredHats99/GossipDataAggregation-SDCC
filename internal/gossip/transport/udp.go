package transport

import (
	"context"
	"fmt"
	"net"
	"time"
)

const DefaultMaxFrameSize = 64 * 1024

type UDPFrameTransport struct {
	conn         net.PacketConn
	maxFrameSize int
}

func NewUDPFrameTransport(bindAddr string, maxFrameSize int) (*UDPFrameTransport, error) {
	conn, err := net.ListenPacket("udp", bindAddr)
	if err != nil {
		return nil, fmt.Errorf("listen udp frame transport: %w", err)
	}
	if maxFrameSize <= 0 {
		maxFrameSize = DefaultMaxFrameSize
	}
	return &UDPFrameTransport{
		conn:         conn,
		maxFrameSize: maxFrameSize,
	}, nil
}

func (t *UDPFrameTransport) SendFrame(ctx context.Context, peer string, frame []byte) error {
	if len(frame) > t.maxFrameSize {
		return ErrFrameTooLarge
	}
	addr, err := net.ResolveUDPAddr("udp", peer)
	if err != nil {
		return fmt.Errorf("resolve udp peer %q: %w", peer, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := t.conn.SetWriteDeadline(deadline); err != nil {
			return fmt.Errorf("set udp write deadline: %w", err)
		}
	} else {
		if err := t.conn.SetWriteDeadline(time.Time{}); err != nil {
			return fmt.Errorf("clear udp write deadline: %w", err)
		}
	}
	if _, err := t.conn.WriteTo(frame, addr); err != nil {
		return fmt.Errorf("write udp frame: %w", err)
	}
	return nil
}

func (t *UDPFrameTransport) NextFrame(ctx context.Context) (string, []byte, error) {
	buf := make([]byte, t.maxFrameSize+1)
	for {
		if err := t.conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
			return "", nil, fmt.Errorf("set udp read deadline: %w", err)
		}
		n, addr, err := t.conn.ReadFrom(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				select {
				case <-ctx.Done():
					return "", nil, ctx.Err()
				default:
					continue
				}
			}
			return "", nil, fmt.Errorf("read udp frame: %w", err)
		}
		if n > t.maxFrameSize {
			return "", nil, ErrFrameTooLarge
		}
		return addr.String(), append([]byte(nil), buf[:n]...), nil
	}
}

func (t *UDPFrameTransport) Close() error {
	return t.conn.Close()
}
