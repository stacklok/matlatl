package relay

import (
	"encoding/binary"
	"hash/fnv"
)

func PlaceTenant(tenant string, nodes []string) string {
	bestNode := ""
	var best uint64
	for _, node := range nodes {
		h := fnv.New64a()
		_, _ = h.Write([]byte(tenant))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(node))
		score := binary.BigEndian.Uint64(h.Sum(nil))
		if bestNode == "" || score > best || score == best && node < bestNode {
			bestNode, best = node, score
		}
	}
	return bestNode
}
