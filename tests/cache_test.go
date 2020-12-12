package tests

import (
	"context"
	"testing"
	"time"

	"bou.ke/monkey"
	"github.com/alicebob/miniredis"
	"github.com/mghayour/mnemosyne"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func NewConfig() *viper.Viper {
	config := viper.New()
	config.SetConfigName("mock_config")
	config.AddConfigPath(".")
	_ = config.ReadInConfig()
	return config
}

type TestTypeUser struct {
	UserName string
	Info     TestTypeUserInfo
	Meta     map[string]string
}
type TestTypeUserInfo struct {
	ClassNumber int32
	RoomNumber  int64
	SchoolName  string
}

func newTestRedis() string {
	mr, err := miniredis.Run()
	if err != nil {
		panic(err)
	}
	return mr.Addr()
}

func setUp() *mnemosyne.MnemosyneInstance {
	config := NewConfig()
	addr := newTestRedis()
	config.SetDefault("cache.result.user-redis.address", addr)
	mnemosyneManager := mnemosyne.NewMnemosyne(config, nil, nil)
	cacheInstance := mnemosyneManager.Select("result")
	return cacheInstance
}
func TestGetAndShouldUpdate(t *testing.T) {
	cacheInstance := setUp()

	cacheCtx, cacheCancelFunc := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cacheCancelFunc()

	testCache := TestTypeUser{
		UserName: "Soheil joon",
		Info: TestTypeUserInfo{
			ClassNumber: 10,
			RoomNumber:  20,
			SchoolName:  "mize baghal",
		},
		Meta: map[string]string{"foo": "A1", "bar": "A2"},
	}
	cacheInstance.Set(cacheCtx, "test_item1", testCache)

	result, shouldUpdate, err := cacheInstance.GetAndShouldUpdate(cacheCtx, "test_item1", &TestTypeUser{})
	logrus.Infof("result %v", result)
	// myCachedData := result.(TestTypeUser)
	myCachedData := *result.(*TestTypeUser)

	assert.Equal(t, testCache, myCachedData)
	assert.Equal(t, nil, err)
	assert.Equal(t, false, shouldUpdate)

	wayback := time.Now().Add(time.Hour * 3)
	patch := monkey.Patch(time.Now, func() time.Time { return wayback })
	defer patch.Unpatch()
	patch2 := monkey.Patch(time.Since, func(t time.Time) time.Duration { return time.Hour * 3 })
	defer patch2.Unpatch()

	// _, shouldUpdate, _ = cacheInstance.GetAndShouldUpdate(cacheCtx, "test_item1", &myCachedData)
	// assert.Equal(t, true, shouldUpdate)
}
