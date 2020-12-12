package mnemosyne

import (
	"context"
	"errors"
	"math/rand"
	"time"

	goCache "github.com/patrickmn/go-cache"
)

type fastMemoryCache struct {
	baseCache
	base     *goCache.Cache
	cacheTTL time.Duration
}

func NewFastMemoryCache(opts *CacheOpts) *fastMemoryCache {
	return &fastMemoryCache{
		baseCache: baseCache{
			layerName:          opts.layerName,
			amnesiaChance:      opts.amnesiaChance,
			compressionEnabled: opts.compressionEnabled,
		},
		base:     goCache.New(opts.cacheTTL, 10*time.Minute),
		cacheTTL: opts.cacheTTL,
	}
}

func (mc *fastMemoryCache) Get(ctx context.Context, key string) (*cachableRet, error) {
	if mc.amnesiaChance > rand.Intn(100) {
		return nil, newAmnesiaError(mc.amnesiaChance)
	}
	res, err := mc.base.Get(key)
	if err == true {
		return nil, errors.New("fastmem get failed")
	}
	return res.(*cachableRet), nil
}

func (mc *fastMemoryCache) Set(ctx context.Context, key string, value interface{}) error {
	mc.base.Set(key, value, goCache.DefaultExpiration)
	return nil
}

func (mc *fastMemoryCache) Delete(ctx context.Context, key string) error {
	mc.base.Delete(key)
	return nil
}

func (mc *fastMemoryCache) Clear() error {
	mc.base.Flush()
	return nil
}

func (mc *fastMemoryCache) TTL(ctx context.Context, key string) time.Duration {
	return time.Second * 0
}

func (mc *fastMemoryCache) Name() string {
	return mc.layerName
}
