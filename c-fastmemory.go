package mnemosyne

import (
	"context"
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
	// Notice: max memory dosent supported by go-cache
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

func (mc *fastMemoryCache) Get(ctx context.Context, key string, refrence interface{}) (*cachable, error) {
	if mc.amnesiaChance > rand.Intn(100) {
		return nil, newAmnesiaError(mc.amnesiaChance)
	}
	val, found := mc.base.Get(key)
	if val == nil || found == false {
		return nil, &ErrCacheMiss{message: "Miss entry at fastmemory layer"}
	}
	res := val.(*cachable)
	return &cachable{
		Time:         res.Time,
		CachedObject: res.CachedObject,
	}, nil
}

func (mc *fastMemoryCache) Set(ctx context.Context, key string, value *cachable) error {
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
	if _, exp, found := mc.base.GetWithExpiration(key); found {
		return time.Until(exp)
	}
	return time.Second * 0
}

func (mc *fastMemoryCache) Name() string {
	return mc.layerName
}
