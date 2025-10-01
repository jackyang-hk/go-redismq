package go_redismq

import (
	"fmt"
	"strings"

	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/redis/go-redis/v9"
)

const DefaultConsumerDelayMilliSeconds = 1500

type Message struct {
	MessageId                 string                 `dc:"MessageId"                    json:"messageId"`
	Topic                     string                 `dc:"Topic"                        json:"topic"`
	Tag                       string                 `dc:"Tag"                          json:"tag"`
	Body                      string                 `dc:"Body"                         json:"body"`
	Key                       string                 `dc:"Key"                          json:"key"`
	StartDeliverTime          int64                  `dc:"Send Time,0-No Delay, Second" json:"startDeliverTime"`
	ReconsumeTimes            int                    `dc:"Reconsume Count"              json:"reconsumeTimes"`
	ReconsumeMax              int                    `dc:"Reconsume Max Count"          json:"reconsumeMax"`
	CustomData                map[string]interface{} `dc:"CustomData"                   json:"customData"`
	SendTime                  int64                  `dc:"Sent Time"                    json:"sendTime"`
	ConsumerDelayMilliSeconds int                    `dc:"Consumer Delay Milliseconds"  json:"consumerDelayMilliSeconds"`
}

type MessageMetaData struct {
	StartDeliverTime          int64                  `dc:"Send Time,0-No Delay, Second" json:"startDeliverTime"`
	ReconsumeTimes            int                    `dc:"Reconsume Count"              json:"reconsumeTimes"`
	ReconsumeMax              int                    `dc:"Reconsume Max Count"          json:"reconsumeMax"`
	CustomData                map[string]interface{} `dc:"CustomData"                   json:"customData"`
	Key                       string                 `dc:"Key"                          json:"key"`
	SendTime                  int64                  `dc:"SendTime"                     json:"sendTime"`
	ConsumerDelayMilliSeconds int                    `dc:"Consumer Delay Milliseconds"  json:"consumerDelayMilliSeconds"`
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
		json, err := gjson.LoadJson([]byte(metadata), true)
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

			message.CustomData = json.Get("customData").Map()
			message.Key = json.Get("key").String()
			message.getUniqueKey()
		}
	}
}
