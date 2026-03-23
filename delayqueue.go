package go_redismq

import (
	"context"
	"fmt"
	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"
	"github.com/redis/go-redis/v9"
	"strconv"
	"time"
)

const MqDelayQueueName = "MQ_DELAY_QUEUE_SET"

var (
	// DelayQueuePollBatchSize is the number of due messages to fetch from the delay queue ZSet per poll (default 10).
	// Can be set before StartRedisMqConsumer or at runtime, e.g. go_redismq.DelayQueuePollBatchSize = 50
	DelayQueuePollBatchSize = 10

	// DelayQueuePollInterval is the delay queue polling interval (default 5s).
	// Can be set before StartRedisMqConsumer or at runtime, e.g. go_redismq.DelayQueuePollInterval = 2 * time.Second
	DelayQueuePollInterval = 5 * time.Second
)

func StartDelayBackgroundThread() {
	go func() {
		for {
			defer func() {
				if exception := recover(); exception != nil {
					fmt.Printf("Redismq pollingCore panic error:%s\n", exception)
					return
				}
			}()
			polling()
			interval := DelayQueuePollInterval
			if interval <= 0 {
				interval = 5 * time.Second
			}
			time.Sleep(interval)
		}
	}()
}

func polling() {
	client := redis.NewClient(GetRedisConfig())

	defer func(client *redis.Client) {
		err := client.Close()
		if err != nil {
			fmt.Printf("RedisMq run error:%s\n", err.Error())
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
			fmt.Printf("RedisMq pollingCore panic error:%s\n", exception)
			return
		}
	}()

	client := redis.NewClient(GetRedisConfig())

	defer func(client *redis.Client) {
		err := client.Close()
		if err != nil {
			fmt.Printf("RedisMq pollingCore error:%s\n", err.Error())
		}
	}(client)
	ctx := context.Background()
	batchSize := DelayQueuePollBatchSize
	if batchSize <= 0 {
		batchSize = 10
	}
	result, err := client.ZRangeByScore(ctx, key, &redis.ZRangeBy{
		Min:    "0",
		Max:    strconv.FormatInt(gtime.Now().Timestamp(), 10),
		Offset: 0,
		Count:  int64(batchSize),
	}).Result()
	if err != nil {
		return
	}
	if len(result) == 0 {
		g.Log().Debugf(ctx, "RedisMq Delay Queue:%s No Queue\n", MqDelayQueueName)
		return
	}
	for _, messageJson := range result {
		var message *Message
		err = gjson.Unmarshal([]byte(messageJson), &message)
		if err != nil {
			fmt.Printf("RedisMq Unmarshal Message Error:[%v]\n", err)
			continue
		}
		removed, remErr := client.ZRem(ctx, key, messageJson).Result()
		dispatch, duplicate, remErrText := resolveDelayQueueDispatchDecision(removed, remErr)
		if dispatch {
			message.StartDeliverTime = 0
			message.MessageId = ""
			send, sendErr := sendMessage(message, "DelayQueue")
			if sendErr != nil {
				g.Log().Errorf(ctx, "RedisMq Delete From Delay Queue,And Send[%v], sendErr:%s\n", send, sendErr.Error())
			} else {
				g.Log().Debugf(ctx, "RedisMq Delete From Delay Queue,And Send:%v\n", send)
			}
		} else if duplicate {
			g.Log().Debugf(ctx, "RedisMq skip duplicate dispatch key:%s\n", key)
		} else if len(remErrText) > 0 {
			g.Log().Debugf(ctx, "RedisMq Delete From Delay Err:%s\n", remErrText)
		}
	}
}

func resolveDelayQueueDispatchDecision(removed int64, remErr error) (dispatch bool, duplicate bool, errText string) {
	if remErr != nil {
		return false, false, remErr.Error()
	}
	if removed == 1 {
		return true, false, ""
	}
	if removed == 0 {
		return false, true, ""
	}
	return false, false, ""
}

func SendDelay(message *Message, delay int64) (bool, error) {
	client := redis.NewClient(GetRedisConfig())

	defer func(client *redis.Client) {
		err := client.Close()
		if err != nil {
			fmt.Printf("RedisMq SendDelay error:%s \n", err.Error())
		}
	}(client)
	messageJson, err := gjson.Marshal(message)
	if err != nil {
		return false, fmt.Errorf("RedisMq SendDelay exception:%s message:%v", err.Error(), message)
	}
	jsonString := string(messageJson)
	score := gtime.Now().Timestamp() + delay
	_, err = client.ZAdd(context.Background(), MqDelayQueueName, redis.Z{
		Score:  float64(score),
		Member: jsonString,
	}).Result()
	if err != nil {
		return false, fmt.Errorf("SendDelay exception:%s message:%v", err.Error(), message)
	}
	//fmt.Printf("RedisMq Push To Deplay Queue,Name[%s],Task[%s],Score[%d],Result[%v]\n", MqDelayQueueName, messageJson, score, result)
	return true, nil
}
