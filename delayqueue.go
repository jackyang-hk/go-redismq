package go_redismq

import (
	"context"
	"strconv"
	"time"

	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/gogf/gf/v2/os/gtime"
	"github.com/redis/go-redis/v9"
)

const (
	MqDelayQueueName = "MQ_DELAY_QUEUE_SET"
)

func StartDelayBackgroundThread() {
	go func() {
		for {
			startDelayBackgroundThreadIteration()

			polling()
			time.Sleep(10 * time.Second)
		}
	}()
}

func startDelayBackgroundThreadIteration() {
	defer func() {
		if exception := recover(); exception != nil {
			logger.Errorf("Redismq polligCore panic error:%s", exception)

			return
		}
	}()
}

func polling() {
	client := redis.NewClient(GetRedisConfig())

	defer func(client *redis.Client) {
		err := client.Close()
		if err != nil {
			logger.Errorf("RedisMq run error:%s", err.Error())
		}
	}(client)

	result, err := client.Keys(context.Background(), MqDelayQueueName).Result()
	if err != nil {
		return
	}

	for _, key := range result {
		pollingCore(key)
	}
}

func pollingCore(key string) {
	defer func() {
		if exception := recover(); exception != nil {
			logger.Errorf("RedisMq polligCore panic error:%s", exception)

			return
		}
	}()

	client := redis.NewClient(GetRedisConfig())

	defer func(client *redis.Client) {
		err := client.Close()
		if err != nil {
			logger.Errorf("RedisMq polligCore error:%s", err.Error())
		}
	}(client)

	ctx := context.Background()

	result, err := client.ZRangeByScore(ctx, key, &redis.ZRangeBy{
		Min:    "0",
		Max:    strconv.FormatInt(gtime.Now().Timestamp(), 10),
		Offset: 0,
		Count:  1,
	}).Result()
	if err != nil {
		return
	}

	if len(result) == 0 {
		logger.Debugf("RedisMq Delay Queue:%s No Queue", MqDelayQueueName)

		return
	}

	for _, messageJson := range result {
		var message *Message

		err = gjson.Unmarshal([]byte(messageJson), &message)
		if err != nil {
			logger.Errorf("RedisMq Unmarshal Message Error:[%v]", err)

			continue
		}

		err = client.ZRem(ctx, key, messageJson).Err()
		if err == nil {
			message.StartDeliverTime = 0
			message.MessageId = ""

			send, sendErr := sendMessage(message, "DelayQueue")
			if sendErr != nil {
				logger.Errorf("RedisMq Delete From Delay Queue,And Send[%v], sendErr:%s", send, sendErr.Error())
			} else {
				logger.Debugf("RedisMq Delete From Delay Queue,And Send:%v", send)
			}
		} else {
			logger.Debugf("RedisMq Delete From Delay Err:%s", err.Error())
		}
	}
}

func SendDelay(message *Message, delay int64) (bool, error) {
	client := redis.NewClient(GetRedisConfig())

	defer func(client *redis.Client) {
		err := client.Close()
		if err != nil {
			logger.Errorf("RedisMq SendDelay error:%s", err.Error())
		}
	}(client)

	messageJson, err := gjson.Marshal(message)
	if err != nil {
		logger.Errorf("RedisMq SendDelay exception:%s message:%v", err.Error(), message)

		return false, err
	}

	jsonString := string(messageJson)
	score := gtime.Now().Timestamp() + delay

	_, err = client.ZAdd(context.Background(), MqDelayQueueName, redis.Z{
		Score:  float64(score),
		Member: jsonString,
	}).Result()
	if err != nil {
		logger.Errorf("SendDelay exception:%s message:%v", err.Error(), message)

		return false, err
	}

	return true, nil
}
