package dragonfly

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	ErrUnableToPingDragonfly = errors.New("unable to ping dragonfly")
)

type DragonflyClient struct {
	Client *redis.Client
	//TODO: consider turning into map
	CacheResultsDuration time.Duration
	KeyPrefix            string
}

func NewDragonflyClient(host string, port int, password string, cacheResultsDuration time.Duration, keyPrefix string) (*DragonflyClient, error) {
	redisOpts := &redis.Options{
		Addr: fmt.Sprintf("%s:%d", host, port),
		DB:   0,
	}

	if password != "" {
		redisOpts.Password = password
	}

	redisClient := redis.NewClient(redisOpts)

	pingCtx, pingCancel := context.WithTimeout(context.Background(), time.Second*10)
	defer pingCancel()

	_, err := redisClient.Ping(pingCtx).Result()
	if err != nil {
		return nil, err
	}

	return &DragonflyClient{
		Client:               redisClient,
		CacheResultsDuration: cacheResultsDuration,
		KeyPrefix:            keyPrefix,
	}, nil
}

func (dc *DragonflyClient) GetClient() *redis.Client {
	return dc.Client
}
