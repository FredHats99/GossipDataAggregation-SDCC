package common

import "regexp"

const (
	ProtocolVersion = "v1"

	AggregateSUM  = "SUM"
	AggregateTOPK = "TOPK"
)

var nodeIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)

func ValidNodeID(nodeID string) bool {
	return nodeIDPattern.MatchString(nodeID)
}

type Aggregator interface {
	Update(value any) (bool, error)
	Merge(peerState any) (bool, error)
	Estimate(opts any) (any, error)
	Serialize() ([]byte, error)
	Deserialize(payload []byte) error
}
