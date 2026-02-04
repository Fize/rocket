package sharding

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewShardManager(t *testing.T) {
	tests := []struct {
		name        string
		shardID     int
		totalShards int
		wantID      int
		wantTotal   int
	}{
		{
			name:        "normal sharding",
			shardID:     0,
			totalShards: 3,
			wantID:      0,
			wantTotal:   3,
		},
		{
			name:        "single shard",
			shardID:     0,
			totalShards: 1,
			wantID:      0,
			wantTotal:   1,
		},
		{
			name:        "zero total shards defaults to 1",
			shardID:     0,
			totalShards: 0,
			wantID:      0,
			wantTotal:   1,
		},
		{
			name:        "negative total shards defaults to 1",
			shardID:     0,
			totalShards: -1,
			wantID:      0,
			wantTotal:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := NewShardManager(tt.shardID, tt.totalShards)
			assert.Equal(t, tt.wantID, sm.ShardID)
			assert.Equal(t, tt.wantTotal, sm.TotalShards)
		})
	}
}

func TestIsResponsibleFor(t *testing.T) {
	tests := []struct {
		name        string
		shardID     int
		totalShards int
		key         string
		want        bool
	}{
		{
			name:        "single shard is always responsible",
			shardID:     0,
			totalShards: 1,
			key:         "any-key",
			want:        true,
		},
		{
			name:        "zero shards (defaults to 1) is always responsible",
			shardID:     0,
			totalShards: 0,
			key:         "test-key",
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := NewShardManager(tt.shardID, tt.totalShards)
			got := sm.IsResponsibleFor(tt.key)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsResponsibleFor_Distribution(t *testing.T) {
	// Test that keys are distributed across shards
	totalShards := 3
	keys := []string{
		"cluster-1", "cluster-2", "cluster-3",
		"cluster-4", "cluster-5", "cluster-6",
		"app-a", "app-b", "app-c",
		"test-key-1", "test-key-2", "test-key-3",
	}

	// Count how many keys each shard is responsible for
	distribution := make(map[int]int)
	for shardID := 0; shardID < totalShards; shardID++ {
		sm := NewShardManager(shardID, totalShards)
		for _, key := range keys {
			if sm.IsResponsibleFor(key) {
				distribution[shardID]++
			}
		}
	}

	// Each key should be handled by exactly one shard
	totalHandled := 0
	for _, count := range distribution {
		totalHandled += count
	}
	assert.Equal(t, len(keys), totalHandled, "each key should be handled by exactly one shard")

	// Check that each shard handles at least some keys (distribution)
	for shardID := 0; shardID < totalShards; shardID++ {
		assert.Greater(t, distribution[shardID], 0, "shard %d should handle at least some keys", shardID)
	}
}

func TestIsResponsibleFor_Deterministic(t *testing.T) {
	// Test that the same key always maps to the same shard
	sm := NewShardManager(0, 3)
	key := "test-cluster-name"

	result1 := sm.IsResponsibleFor(key)
	result2 := sm.IsResponsibleFor(key)
	result3 := sm.IsResponsibleFor(key)

	assert.Equal(t, result1, result2)
	assert.Equal(t, result2, result3)
}

func TestIsResponsibleFor_DifferentShards(t *testing.T) {
	// Test that different shards report different responsibilities for the same key
	totalShards := 3
	key := "test-cluster"

	responsibleCount := 0
	for shardID := 0; shardID < totalShards; shardID++ {
		sm := NewShardManager(shardID, totalShards)
		if sm.IsResponsibleFor(key) {
			responsibleCount++
		}
	}

	// Exactly one shard should be responsible for each key
	assert.Equal(t, 1, responsibleCount, "exactly one shard should be responsible for each key")
}
