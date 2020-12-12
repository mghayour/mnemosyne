package mnemosyne

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
)

type ICache interface {
	Get(context.Context, string) (*cachable, error)
	Set(context.Context, string, interface{}) error
	Delete(context.Context, string) error
	Clear() error
	TTL(context.Context, string) time.Duration
	Name() string
}

type MemoryOpts struct {
	maxMem int
}

type CacheOpts struct {
	layerName          string
	layerType          string
	redisOpts          RedisOpts
	memOpts            MemoryOpts
	amnesiaChance      int
	compressionEnabled bool
	cacheTTL           time.Duration
}

type baseCache struct {
	layerName          string
	amnesiaChance      int
	compressionEnabled bool
}

func NewCacheLayer(opts *CacheOpts, watcher ITimer) ICache {
	layerType := opts.layerType
	if layerType == "memory" {
		return NewInMemoryCache(opts)
	} else if layerType == "tiny" {
		return NewTinyCache(opts)
	} else if layerType == "redis" {
		return NewShardedClusterRedisCache(opts, watcher)
	} else if layerType == "gaurdian" {
		// to preserve backward-compatibility
		return NewShardedClusterRedisCache(opts, watcher)
	} else if layerType == "fastmemory" {
		return NewFastMemoryCache(opts)
	}
	logrus.Errorf("Malformed: Unknown cache type %s", layerType)
	return nil
}
