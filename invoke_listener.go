package go_redismq

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/redis/go-redis/v9"
)

type MessageInvokeListener struct {
}

func (t MessageInvokeListener) GetTopic() string {
	return TopicInternal
}

func (t MessageInvokeListener) GetTag() string {
	return TagInvoke
}

func (t MessageInvokeListener) Consume(ctx context.Context, message *Message) Action {
	var req *InvoiceRequest

	err := UnmarshalFromJsonString(message.Body, &req)
	if err != nil {
		logger.Errorf("MessageInvokeListener UnmarshalFromJsonString Body error %s", err.Error())
		//ignore
		return CommitMessage
	}

	if req == nil || req.Group != Group || len(req.MessageId) == 0 || len(req.Method) == 0 {
		if req.Group != Group {
			logger.Infof("MessageInvokeListener Skip Request Group:%s Request Group:%s", Group, req.Group)
		} else {
			logger.Errorf("MessageInvokeListener Group:%s Invalid Request:%s", Group, MarshalToJsonString(req))
		}
		//ignore
		return CommitMessage
	}

	res := &InvoiceResponse{}
	client := redis.NewClient(GetRedisConfig())

	defer func(client *redis.Client) {
		err := client.Close()
		if err != nil {
			logger.Errorf("MQStream Closs Redis Stream Client error:%s", err.Error())
		}
	}(client)

	replyChannel := getReplyChannel(req)

	defer func() {
		if exception := recover(); exception != nil {
			if v, ok := exception.(error); ok && gerror.HasStack(v) {
				err = v
			} else {
				err = gerror.NewCodef(gcode.CodeInternalPanic, "%+v", exception)
			}

			logger.Errorf("MQStream invoke err method:%s panic:%v", req.Method, err)
			logger.Errorf("MQStream invoke err stack trace:\n%s", debug.Stack())

			res.Response = err.Error()
			res.Status = false
			client.Publish(ctx, replyChannel, MarshalToJsonString(res))

			return
		}
	}()

	if op, ok := invokeMap[req.Method]; ok {
		// invoke method
		response, err := op(ctx, req.Request)
		if err != nil {
			res.Response = err.Error()
			res.Status = false
			client.Publish(ctx, replyChannel, MarshalToJsonString(res))
		} else {
			res.Response = response
			res.Status = true
			client.Publish(ctx, replyChannel, MarshalToJsonString(res))
		}
	} else {
		res.Response = "error: method not found"
		res.Status = false
		client.Publish(ctx, replyChannel, MarshalToJsonString(res))
	}

	return CommitMessage
}

func getReplyChannel(req *InvoiceRequest) string {
	replyChannel := fmt.Sprintf("RedisMQ:%s_%s:%s", req.Group, req.Method, req.MessageId)

	return replyChannel
}

func init() {
	RegisterListener(&MessageInvokeListener{})
	if logger != nil {
		logger.Infof("MessageInvokeListener RegisterListener")
	}
}

var invokeMap = make(map[string]func(ctx context.Context, request interface{}) (response interface{}, err error))

func RegisterInvoke(methodName string, op func(ctx context.Context, request interface{}) (response interface{}, err error)) {
	if len(methodName) <= 0 || op == nil {
		if logger != nil {
			logger.Errorf("MQStream RegisterInvoke error methodName:%s or op:%p is nil", methodName, op)
		}
	} else if _, ok := invokeMap[methodName]; ok {
		if logger != nil {
			logger.Warnf("MQStream RegisterInvoke error exist Old One:%s for op:%p", methodName, op)
		}
	} else {
		invokeMap[methodName] = op
		if logger != nil {
			logger.Infof("MQStream RegisterInvoke methodName:%s for op:%p", methodName, op)
		}
	}
}

func keepAliveMessageInvokeListener() {
	client := redis.NewClient(GetRedisConfig())
	defer func(client *redis.Client) {
		err := client.Close()
		if err != nil {
			logger.Errorf("MQStream Closs Redis Stream Client error:%s", err.Error())
		}
	}(client)

	client.Set(context.Background(), "MessageInvokeGroup:"+Group, true, 300*time.Second)

	for {
		time.Sleep(60 * time.Second)
		client.Expire(context.Background(), "MessageInvokeGroup:"+Group, 300*time.Second)
	}
}
