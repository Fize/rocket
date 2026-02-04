package sharding

import (
	"hash/fnv"
)

// ShardManager determines if the current manager instance is responsible for a given resource
type ShardManager struct {
	ShardID     int
	TotalShards int
}

// NewShardManager creates a new ShardManager
func NewShardManager(shardID, totalShards int) *ShardManager {
	if totalShards < 1 {
		totalShards = 1
	}
	return &ShardManager{
		ShardID:     shardID,
		TotalShards: totalShards,
	}
}

// IsResponsibleFor returns true if the current shard is responsible for the given key
func (s *ShardManager) IsResponsibleFor(key string) bool {
	if s.TotalShards <= 1 {
		return true
	}
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32())%s.TotalShards == s.ShardID
}
