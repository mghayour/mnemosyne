package mnemosyne

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-redis/redis"
	"github.com/pkg/errors"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// Mnemosyne is the parent object which holds all cache instances
type Mnemosyne struct {
	childs map[string]*MnemosyneInstance
}

// MnemosyneInstance is an instance of a multi-layer cache
type MnemosyneInstance struct {
	name         string
	cacheLayers  []ICache
	cacheWatcher ICounter
	softTTL      time.Duration
}

// ErrCacheMiss is the Error returned when a cache miss happens
type ErrCacheMiss struct {
	message string
}

func (e *ErrCacheMiss) Error() string {
	return e.message
}

// NewMnemosyne initializes the Mnemosyne object which holds all the cache instances
func NewMnemosyne(config *viper.Viper, commTimer ITimer, cacheHitCounter ICounter) *Mnemosyne {
	if commTimer == nil {
		commTimer = NewDummyTimer()
	}
	if cacheHitCounter == nil {
		cacheHitCounter = NewDummyCounter()
	}
	cacheConfigs := config.GetStringMap("cache")
	caches := make(map[string]*MnemosyneInstance, len(cacheConfigs))
	for cacheName := range cacheConfigs {
		caches[cacheName] = newMnemosyneInstance(cacheName, config, commTimer, cacheHitCounter)
	}
	return &Mnemosyne{
		childs: caches,
	}
}

// Select returns a cache instance selected by name
func (m *Mnemosyne) Select(cacheName string) *MnemosyneInstance {
	return m.childs[cacheName]
}

func newMnemosyneInstance(name string, config *viper.Viper, commTimer ITimer, hitCounter ICounter) *MnemosyneInstance {
	configKeyPrefix := fmt.Sprintf("cache.%s", name)
	layerNames := config.GetStringSlice(configKeyPrefix + ".layers")
	cacheLayers := make([]ICache, len(layerNames))
	for i, layerName := range layerNames {
		keyPrefix := configKeyPrefix + "." + layerName
		layerOptions := &CacheOpts{
			layerType:          config.GetString(keyPrefix + ".type"),
			layerName:          layerName,
			cacheTTL:           config.GetDuration(keyPrefix + ".ttl"),
			amnesiaChance:      config.GetInt(keyPrefix + ".amnesia"),
			compressionEnabled: config.GetBool(keyPrefix + ".compression"),
			memOpts: MemoryOpts{
				maxMem: config.GetInt(keyPrefix + ".max-memory"),
			},
			redisOpts: RedisOpts{
				db:          config.GetInt(keyPrefix + ".db"),
				idleTimeout: config.GetDuration(keyPrefix + ".idle-timeout"),
				shards:      make([]*RedisClusterAddress, 1),
			},
		}
		if layerOptions.layerType == "redis" {
			layerOptions.redisOpts.shards[0].MasterAddr = config.GetString(keyPrefix + ".address")
		} else if layerOptions.layerType == "gaurdian" {
			// to preserve backward-compatibility
			layerOptions.redisOpts.shards[0].MasterAddr = config.GetString(keyPrefix + ".address")
			layerOptions.redisOpts.shards[0].SlaveAddrs = config.GetStringSlice(keyPrefix + ".slaves")
		} else if layerOptions.layerType == "rediscluster" {
			err := config.UnmarshalKey(keyPrefix+".cluster", &layerOptions.redisOpts.shards)
			if err != nil {
				logrus.WithError(err).Error("Error reading redis cluster config")
			}
		}
		cacheLayers[i] = NewCacheLayer(layerOptions, commTimer)

	}
	return &MnemosyneInstance{
		name:         name,
		cacheLayers:  cacheLayers,
		cacheWatcher: hitCounter,
		softTTL:      config.GetDuration(configKeyPrefix + ".soft-ttl"),
	}
}

func (mn *MnemosyneInstance) get(ctx context.Context, key string) (*cachableRet, error) {
	cacheErrors := make([]error, len(mn.cacheLayers))
	var result *cachableRet
	for i, layer := range mn.cacheLayers {
		result, cacheErrors[i] = layer.Get(ctx, key)
		if cacheErrors[i] == nil {
			go func() {
				mn.fillUpperLayers(key, result, i)
				mn.cacheWatcher.Inc(mn.name, fmt.Sprintf("layer%d", i))
			}()
			return result, nil
		}
	}
	go mn.cacheWatcher.Inc(mn.name, "miss")
	return nil, &ErrCacheMiss{message: "Miss"} // FIXME: better Error combination
}

// get from all layers and replace older data with new one
func (mn *MnemosyneInstance) getAndSyncLayers(ctx context.Context, key string) (*cachableRet, error) {
	cacheResults := make([]*cachableRet, len(mn.cacheLayers))
	var result *cachableRet
	var resultLayer int
	for i, layer := range mn.cacheLayers {
		cacheResults[i], _ = layer.withContext(ctx).get(key)
		if cacheResults[i] != nil &&
			(result == nil ||
				cacheResults[i].Time.After(result.Time)) {
			result = cacheResults[i]
			resultLayer = i
		}
	}
	if result == nil {
		go mn.cacheWatcher.Inc(mn.name, "miss")
		return nil, &ErrCacheMiss{message: "Miss"} // FIXME: better Error combination
	}
	for i, layer := range mn.cacheLayers {
		if cacheResults[i] == nil || cacheResults[i].Time.Before(result.Time) {
			go layer.set(key, *result)
		}
	}

	go mn.cacheWatcher.Inc(mn.name, fmt.Sprintf("layer%d", resultLayer))
	return result, nil
}

// Get retrieves the value for key
func (mn *MnemosyneInstance) Get(ctx context.Context, key string, ref interface{}) error {
	cachableObj, err := mn.get(ctx, key)
	if err != nil {
		return err
	}

	if cachableObj == nil || cachableObj.CachedObject == nil {
		logrus.Errorf("nil object found in cache %s ! %v", key, cachableObj)
		return errors.New("nil found")
	}

	err = json.Unmarshal(*cachableObj.CachedObject, ref)
	if err != nil {
		return err
	}
	return nil
}

// GetAndShouldUpdate retrieves the value for key and also shows whether the soft-TTL of that key has passed or not
func (mn *MnemosyneInstance) GetAndShouldUpdate(ctx context.Context, key string, ref interface{}) (bool, error) {
	cachableObj, err := mn.get(ctx, key)
	if err == redis.Nil {
		return true, err
	} else if err != nil {
		return false, err
	}

	if cachableObj == nil || cachableObj.CachedObject == nil {
		logrus.Errorf("nil object found in cache %s ! %v", key, cachableObj)
		return false, errors.New("nil found")
	}

	err = json.Unmarshal(*cachableObj.CachedObject, ref)
	if err != nil {
		return false, err
	}
	dataAge := time.Since(cachableObj.Time)
	go mn.monitorDataHotness(dataAge)
	shouldUpdate := dataAge > mn.softTTL
	return shouldUpdate, nil
}

// ShouldUpdate shows whether the soft-TTL of a key has passed or not
func (mn *MnemosyneInstance) ShouldUpdate(ctx context.Context, key string) (bool, error) {
	cachableObj, err := mn.get(ctx, key)
	if err == redis.Nil {
		return true, err
	} else if err != nil {
		return false, err
	}

	if cachableObj == nil || cachableObj.CachedObject == nil {
		logrus.Errorf("nil object found in cache %s ! %v", key, cachableObj)
		return false, errors.New("nil found")
	}

	shouldUpdate := time.Since(cachableObj.Time) > mn.softTTL

	return shouldUpdate, nil
}

// ShouldUpdateDeep checks all layers for newer result and will sync older cache layers
func (mn *MnemosyneInstance) ShouldUpdateDeep(ctx context.Context, key string) (bool, error) {
	cachableObj, err := mn.getAndSyncLayers(ctx, key)
	if err == redis.Nil {
		return true, err
	} else if err != nil {
		return false, err
	}

	if cachableObj == nil || cachableObj.CachedObject == nil {
		logrus.Errorf("nil object found in cache %s ! %v", key, cachableObj)
		return false, errors.New("nil found")
	}

	shouldUpdate := time.Now().Sub(cachableObj.Time) > mn.softTTL

	return shouldUpdate, nil
}

// Set sets the value for a key in all layers of the cache instance
func (mn *MnemosyneInstance) Set(ctx context.Context, key string, value interface{}) error {
	if value == nil {
		return fmt.Errorf("cannot set nil value in cache")
	}

	toCache := cachable{
		CachedObject: value,
		Time:         time.Now(),
	}
	cacheErrors := make([]error, len(mn.cacheLayers))
	errorStrings := make([]string, len(mn.cacheLayers))
	haveErorr := false
	for i, layer := range mn.cacheLayers {
		cacheErrors[i] = layer.Set(ctx, key, toCache)
		if cacheErrors[i] != nil {
			errorStrings[i] = cacheErrors[i].Error()
			haveErorr = true
		}
	}
	if haveErorr {
		return fmt.Errorf(strings.Join(errorStrings, ";"))
	}
	return nil
}

// TTL returns the TTL of the first accessible data instance as well as the layer it was found on
func (mn *MnemosyneInstance) TTL(ctx context.Context, key string) (int, time.Duration) {
	for i, layer := range mn.cacheLayers {
		dur := layer.TTL(ctx, key)
		if dur > 0 {
			return i, dur
		}
	}
	return -1, time.Second * 0
}

// Delete removes a key from all the layers (if exists)
func (mn *MnemosyneInstance) Delete(ctx context.Context, key string) error {
	cacheErrors := make([]error, len(mn.cacheLayers))
	errorStrings := make([]string, len(mn.cacheLayers))
	haveErorr := false
	for i, layer := range mn.cacheLayers {
		cacheErrors[i] = layer.Delete(ctx, key)
		if cacheErrors[i] != nil {
			errorStrings[i] = cacheErrors[i].Error()
			haveErorr = true
		}
	}
	if haveErorr {
		return fmt.Errorf(strings.Join(errorStrings, ";"))
	}
	return nil
}

// Flush completly clears a single layer of the cache
func (mn *MnemosyneInstance) Flush(targetLayerName string) error {
	for _, layer := range mn.cacheLayers {
		if layer.Name() == targetLayerName {
			return layer.Clear()
		}
	}
	return fmt.Errorf("Layer Named: %v Not Found", targetLayerName)
}

func (mn *MnemosyneInstance) fillUpperLayers(key string, value *cachableRet, layer int) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for i := layer - 1; i >= 0; i-- {
		if value == nil {
			continue
		}
		err := mn.cacheLayers[i].Set(ctx, key, *value)
		if err != nil {
			logrus.Errorf("failed to fill layer %d : %v", i, err)
		}
	}
}

func (mn *MnemosyneInstance) monitorDataHotness(age time.Duration) {
	if age <= mn.softTTL {
		mn.cacheWatcher.Inc(mn.name+"-hotness", "hot")
	} else if age <= mn.softTTL*2 {
		mn.cacheWatcher.Inc(mn.name+"-hotness", "warm")
	} else {
		mn.cacheWatcher.Inc(mn.name+"-hotness", "cold")
	}
}
