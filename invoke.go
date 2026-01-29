package go_redismq

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type InvoiceRequest struct {
	MessageId string      `json:"messageId"`
	Group     string      `json:"group"`
	Method    string      `json:"method"`
	Request   interface{} `json:"request"`
}

type InvoiceResponse struct {
	Status   bool        `json:"status"`
	Response interface{} `json:"response"`
}

func listenForResponse(ctx context.Context, req *InvoiceRequest, responseChan chan *InvoiceResponse) {
	client := redis.NewClient(GetRedisConfig())

	defer func(client *redis.Client) {
		err := client.Close()
		if err != nil {
			logger.Errorf("MQStream Closs Redis Stream Client error:%s", err.Error())
		}
	}(client)

	replyChannel := getReplyChannel(req)
	logger.Debugf("MethodInvoke waiting for replyChannel:%s", replyChannel)

	pubSub := client.Subscribe(ctx, replyChannel)
	defer func(pubSub *redis.PubSub) {
		err := pubSub.Close()
		if err != nil {
			logger.Errorf("Error pubSub: %s", err.Error())
		}
	}(pubSub)

	ch := pubSub.Channel()

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				logger.Infof("listenForResponse channel closed")

				return
			}

			var res *InvoiceResponse

			err := json.Unmarshal([]byte(msg.Payload), &res)
			if err != nil {
				logger.Errorf("Error deserializing response: %s", err.Error())

				return
			}

			logger.Debugf("MethodInvoke get response:%s replyChannel:%s", MarshalToJsonString(res), replyChannel)

			responseChan <- res

			return
		case <-ctx.Done():
			logger.Infof("listenForResponse timeout or cancelled")

			return
		}
	}
}

func Invoke(ctx context.Context, req *InvoiceRequest, timeoutSeconds int) *InvoiceResponse {
	startTime := time.Now()

	if timeoutSeconds <= 0 {
		timeoutSeconds = 15
	}

	invokeId := fmt.Sprintf("%s%d", GenerateRandomAlphanumeric(6), CurrentTimeMillis())
	req.MessageId = invokeId

	//check group listener exist
	client := redis.NewClient(GetRedisConfig())

	defer func(client *redis.Client) {
		err := client.Close()
		if err != nil {
			logger.Errorf("MQStream Closs Redis Stream Client error:%s", err.Error())
		}
	}(client)

	data, err := client.Get(ctx, "MessageInvokeGroup:"+req.Group).Result()
	if err != nil {
		return &InvoiceResponse{
			Status:   false,
			Response: "Invoke get group:" + err.Error(),
		}
	}

	if len(data) == 0 {
		return &InvoiceResponse{
			Status:   false,
			Response: "Invoke Group Not Found:" + req.Group,
		}
	}

	responseChan := make(chan *InvoiceResponse)
	go listenForResponse(ctx, req, responseChan)

	send, err := Send(&Message{
		Topic: TopicInternal,
		Tag:   TagInvoke,
		Body:  MarshalToJsonString(req),
	})
	if err != nil {
		return &InvoiceResponse{
			Status:   false,
			Response: "Invoke error:" + err.Error(),
		}
	} else if !send {
		return &InvoiceResponse{
			Status:   false,
			Response: "Invoke send failed",
		}
	}

	logger.Infof("RedisMQ:Measure:Invoke After Send Message cost：%s", time.Since(startTime))

	go func() {
		time.Sleep(time.Duration(timeoutSeconds) * time.Second)

		select {
		case <-ctx.Done():
			return
		case responseChan <- &InvoiceResponse{
			Status:   false,
			Response: "Timeout",
		}:
		}
	}()

	select {
	case <-ctx.Done():
		logger.Infof("RedisMQ:Measure:Invoke cost：%s", time.Since(startTime))

		return &InvoiceResponse{
			Status:   false,
			Response: "Invoke context timeout",
		}
	case response := <-responseChan:
		logger.Infof("RedisMQ:Measure:Invoke cost：%s", time.Since(startTime))

		return response
	}
}
