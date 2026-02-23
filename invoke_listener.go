package go_redismq

import (
	"context"
	"fmt"
	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/redis/go-redis/v9"
	"runtime/debug"
	"time"
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
		g.Log().Errorf(ctx, "MessageInvokeListener UnmarshalFromJsonString Body error %s", err.Error())
		//ignore
		return CommitMessage
	}
	if req == nil || req.Group != Group || len(req.MessageId) == 0 || len(req.Method) == 0 {
		if req.Group != Group {
			g.Log().Infof(ctx, "MessageInvokeListener Skip Request Group:%s Request Group:%s", Group, req.Group)
		} else {
			g.Log().Errorf(ctx, "MessageInvokeListener Group:%s Invalid Request:%s", Group, MarshalToJsonString(req))
		}
		//ignore
		return CommitMessage
	}
	res := &InvoiceResponse{}
	client := redis.NewClient(GetRedisConfig())

	defer func(client *redis.Client) {
		err := client.Close()
		if err != nil {
			fmt.Printf("MQStream Closs Redis Stream Client error:%s\n", err.Error())
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
			fmt.Printf("MQStream invoke err method:%s panic:%v\n", req.Method, err)
			fmt.Printf("MQStream invoke err stack trace:\n%s", debug.Stack())
			res.Response = fmt.Sprintf("%s", err.Error())
			res.Status = false
			client.Publish(ctx, replyChannel, MarshalToJsonString(res))
			return
		}
	}()
	if op, ok := invokeMap[req.Method]; ok {
		// invoke method
		response, err := op(ctx, req.Request)
		if err != nil {
			res.Response = fmt.Sprintf("%s", err.Error())
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
	fmt.Println("MessageInvokeListener RegisterListener")

}

var invokeMap = make(map[string]func(ctx context.Context, request interface{}) (response interface{}, err error))

func RegisterInvoke(methodName string, op func(ctx context.Context, request interface{}) (response interface{}, err error)) {
	if len(methodName) <= 0 || op == nil {
		fmt.Printf("MQStream RegisterInvoke error methodName:%s or op:%p is nil\n", methodName, op)
	} else if _, ok := invokeMap[methodName]; ok {
		fmt.Printf("MQStream RegisterInvoke error exist Old One:%s for op:%p\n", methodName, op)
	} else {
		invokeMap[methodName] = op
		fmt.Printf("MQStream RegisterInvoke methodName:%s for op:%p\n", methodName, op)
	}
}

func keepAliveMessageInvokeListener() {
	key := fmt.Sprintf("MessageInvokeGroup:%s", Group)
	for {
		func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Printf("MQStream keepAliveMessageInvokeListener panic recovered: %v\n", r)
				}
			}()
			client := redis.NewClient(GetRedisConfig())
			defer func(client *redis.Client) {
				err := client.Close()
				if err != nil {
					fmt.Printf("MQStream keepAliveMessageInvokeListener Close Redis Client error:%s\n", err.Error())
				}
			}(client)
			ctx := context.Background()
			if err := client.Set(ctx, key, true, 300*time.Second).Err(); err != nil {
				fmt.Printf("MQStream keepAliveMessageInvokeListener Set failed: %v\n", err)
				return
			}
			for {
				time.Sleep(60 * time.Second)
				if err := client.Expire(ctx, key, 300*time.Second).Err(); err != nil {
					fmt.Printf("MQStream keepAliveMessageInvokeListener Expire failed: %v\n", err)
					return // exit inner loop, outer for will recreate client and retry
				}
			}
		}()
		time.Sleep(5 * time.Second) // brief wait before retry after failure
	}
}
