package transport

import "context"

// EncodingSender adapts a byte-oriented sender and a codec to the Sender
// interface expected by gossip logic.
type EncodingSender struct {
	frameSender FrameSender
	codec       Codec
}

func NewEncodingSender(frameSender FrameSender, codec Codec) (*EncodingSender, error) {
	if frameSender == nil {
		return nil, ErrNilFrameSender
	}
	if codec == nil {
		return nil, ErrNilCodec
	}
	return &EncodingSender{
		frameSender: frameSender,
		codec:       codec,
	}, nil
}

func (s *EncodingSender) Send(ctx context.Context, peer string, message Envelope) error {
	frame, err := s.codec.Encode(message)
	if err != nil {
		return err
	}
	return s.frameSender.SendFrame(ctx, peer, frame)
}

// DecodingReceiver adapts a byte-oriented receiver and a codec to the Receiver
// interface consumed by gossip logic.
type DecodingReceiver struct {
	frameReceiver FrameReceiver
	codec         Codec
}

func NewDecodingReceiver(frameReceiver FrameReceiver, codec Codec) (*DecodingReceiver, error) {
	if frameReceiver == nil {
		return nil, ErrNilFrameReceiver
	}
	if codec == nil {
		return nil, ErrNilCodec
	}
	return &DecodingReceiver{
		frameReceiver: frameReceiver,
		codec:         codec,
	}, nil
}

func (r *DecodingReceiver) Next(ctx context.Context) (Envelope, error) {
	_, frame, err := r.frameReceiver.NextFrame(ctx)
	if err != nil {
		return Envelope{}, err
	}
	return r.codec.Decode(frame)
}

func (r *DecodingReceiver) Close() error {
	return r.frameReceiver.Close()
}
