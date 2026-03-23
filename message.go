package go_redismq

import (
	"fmt"
	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/redis/go-redis/v9"
	"strings"
)

const DefaultConsumerDelayMilliSeconds = 1500

type Message struct {
	MessageId                 string                 `json:"messageId" dc:"MessageId"`
	Topic                     string                 `json:"topic" dc:"Topic"`
	Tag                       string                 `json:"tag" dc:"Tag"`
	Body                      string                 `json:"body" dc:"Body"`
	Key                       string                 `json:"key" dc:"Key"`
	StartDeliverTime          int64                  `json:"startDeliverTime" dc:"Send Time,0-No Delay，Second"`
	ReconsumeTimes            int                    `json:"reconsumeTimes" dc:"Reconsume Count"`
	ReconsumeMax              int                    `json:"reconsumeMax" dc:"Reconsume Max Count"`
	CustomData                map[string]interface{} `json:"customData" dc:"CustomData"`
	SendTime                  int64                  `json:"sendTime" dc:"Sent Time"`
	ConsumerDelayMilliSeconds int                    `json:"consumerDelayMilliSeconds" dc:"Consumer Delay Milliseconds"`
	NextRetryDelaySeconds     int64                  `json:"nextRetryDelaySeconds" dc:"Next Retry Delay Seconds"`
	NextDeliverAt             int64                  `json:"nextDeliverAt" dc:"Next Deliver At, Unix Seconds"`
}

type MessageMetaData struct {
	StartDeliverTime          int64                  `json:"startDeliverTime" dc:"Send Time,0-No Delay，Second"`
	ReconsumeTimes            int                    `json:"reconsumeTimes" dc:"Reconsume Count"`
	ReconsumeMax              int                    `json:"reconsumeMax" dc:"Reconsume Max Count"`
	CustomData                map[string]interface{} `json:"customData" dc:"CustomData"`
	Key                       string                 `json:"key" dc:"Key"`
	SendTime                  int64                  `json:"sendTime" dc:"SendTime"`
	ConsumerDelayMilliSeconds int                    `json:"consumerDelayMilliSeconds" dc:"Consumer Delay Milliseconds"`
	NextRetryDelaySeconds     int64                  `json:"nextRetryDelaySeconds" dc:"Next Retry Delay Seconds"`
	NextDeliverAt             int64                  `json:"nextDeliverAt" dc:"Next Deliver At, Unix Seconds"`
}

func NewRedisMQMessage(topicWrapper MQTopicEnum, body string) *Message {
	return &Message{
		Topic:    topicWrapper.Topic,
		Tag:      topicWrapper.Tag,
		Body:     body,
		SendTime: CurrentTimeMillis(),
	}
}

func (message *Message) getUniqueKey() string {
	if message.CustomData == nil {
		message.CustomData = make(map[string]interface{})
	}
	uniqueKey := ""
	if value, ok := message.CustomData["uniqueKey"].(string); ok && len(value) > 0 {
		uniqueKey = value
	}
	if len(uniqueKey) == 0 && len(message.MessageId) > 0 {
		message.CustomData["uniqueKey"] = message.MessageId
		return message.MessageId
	} else {
		return uniqueKey
	}
}

func (message *Message) isBoardCastingMessage() bool {
	if value, ok := message.CustomData["messageModel"].(string); ok {
		return strings.Compare(value, "BROADCASTING") == 0
	} else {
		return false
	}
}

func (message *Message) getDescription() string {
	return fmt.Sprintf("%s %s %s", message.MessageId, message.Topic, message.Tag)
}

func (message *Message) toStreamAddArgsValues(stream string) *redis.XAddArgs {
	if message.ConsumerDelayMilliSeconds == 0 {
		message.ConsumerDelayMilliSeconds = DefaultConsumerDelayMilliSeconds
	}
	metadata := MessageMetaData{
		StartDeliverTime:          message.StartDeliverTime,
		ReconsumeTimes:            message.ReconsumeTimes,
		CustomData:                message.CustomData,
		Key:                       message.Key,
		ConsumerDelayMilliSeconds: message.ConsumerDelayMilliSeconds,
		SendTime:                  CurrentTimeMillis(),
		NextRetryDelaySeconds:     message.NextRetryDelaySeconds,
		NextDeliverAt:             message.NextDeliverAt,
	}
	metaJson, _ := gjson.Marshal(metadata)
	var values = map[string]interface{}{
		"topic":    message.Topic,
		"tag":      message.Tag,
		"body":     message.Body,
		"metadata": string(metaJson),
	}
	return &redis.XAddArgs{
		Stream: stream,
		Values: values,
	}
}

func (message *Message) passStreamMessage(value map[string]interface{}) {
	if target, ok := value["topic"].(string); ok {
		message.Topic = target
	}
	if target, ok := value["tag"].(string); ok {
		message.Tag = target
	}
	if target, ok := value["body"].(string); ok {
		message.Body = target
	}
	var metadata string
	if target, ok := value["metadata"].(string); ok {
		metadata = target
	}
	if len(metadata) > 0 {
		json, err := gjson.LoadJson(metadata, true)
		if err == nil {
			defer func() {
				if exception := recover(); exception != nil {
					fmt.Printf("Redismq passStreamMessage panic error:%s\n", exception)
					return
				}
			}()
			message.ReconsumeTimes = json.Get("reconsumeTimes").Int()
			message.ReconsumeMax = json.Get("reconsumeMax").Int()
			message.StartDeliverTime = json.Get("startDeliverTime").Int64()
			message.SendTime = json.Get("sendTime").Int64()
			if json.Contains("consumerDelayMilliSeconds") {
				if consumerDelayMilliSeconds := json.Get("consumerDelayMilliSeconds"); consumerDelayMilliSeconds != nil {
					message.ConsumerDelayMilliSeconds = consumerDelayMilliSeconds.Int()
				}
			}
			if json.Contains("nextRetryDelaySeconds") {
				if nextRetryDelaySeconds := json.Get("nextRetryDelaySeconds"); nextRetryDelaySeconds != nil {
					message.NextRetryDelaySeconds = nextRetryDelaySeconds.Int64()
				}
			}
			if json.Contains("nextDeliverAt") {
				if nextDeliverAt := json.Get("nextDeliverAt"); nextDeliverAt != nil {
					message.NextDeliverAt = nextDeliverAt.Int64()
				}
			}
			message.CustomData = json.Get("customData").Map()
			message.Key = json.Get("key").String()
			message.getUniqueKey()
		}
	}
}
