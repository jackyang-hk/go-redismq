package go_redismq

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/glog"
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
			fmt.Printf("MQStream Closs Redis Stream Client error:%s\n", err.Error())
		}
	}(client)

	replyChannel := getReplyChannel(req)
	g.Log().Debugf(ctx, "MethodInvoke waiting for replyChannel:%s", replyChannel)

	pubSub := client.Subscribe(ctx, replyChannel)
	defer func(pubSub *redis.PubSub) {
		err := pubSub.Close()
		if err != nil {
			g.Log().Errorf(ctx, "Error pubSub: %s\n", err.Error())
		}
	}(pubSub)

	ch := pubSub.Channel()

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				g.Log().Infof(ctx, "listenForResponse channel closed")

				return
			}

			var res *InvoiceResponse

			err := json.Unmarshal([]byte(msg.Payload), &res)
			if err != nil {
				g.Log().Errorf(ctx, "Error deserializing response: %s\n", err.Error())

				return
			}

			g.Log().Debugf(ctx, "MethodInvoke get response:%s replyChannel:%s", MarshalToJsonString(res), replyChannel)

			responseChan <- res

			return
		case <-ctx.Done():
			g.Log().Infof(ctx, "listenForResponse timeout or cancelled")

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

	// check group listener exist
	client := redis.NewClient(GetRedisConfig())

	defer func(client *redis.Client) {
		err := client.Close()
		if err != nil {
			fmt.Printf("MQStream Closs Redis Stream Client error:%s\n", err.Error())
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

	glog.Infof(ctx, "RedisMQ:Measure:Invoke After Send Message cost：%s \n", time.Since(startTime))

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
		glog.Infof(ctx, "RedisMQ:Measure:Invoke cost：%s \n", time.Since(startTime))

		return &InvoiceResponse{
			Status:   false,
			Response: "Invoke context timeout",
		}
	case response := <-responseChan:
		glog.Infof(ctx, "RedisMQ:Measure:Invoke cost：%s \n", time.Since(startTime))

		return response
	}
}
