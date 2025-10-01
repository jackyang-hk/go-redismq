package go_redismq

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"
	"github.com/redis/go-redis/v9"
)

var consumerName = ""

func StartRedisMqConsumer() {
	go func() {
		innerSettingConsumerName()

		if len(consumerName) == 0 {
			fmt.Println("MQStream StartRedisMqConsumer Failed While ConsumerName Invalid")

			return
		}

		StartDelayBackgroundThread()
		fmt.Println("MQStream Start Delay Queue!")

		deathQueueName := GetDeathQueueName()
		createStreamGroup(deathQueueName, "death_message")
		fmt.Printf("MQStream Init Death Queue deathQueueName:%s", deathQueueName)
		innerLoadConsumer()
		fmt.Println("MQStream Finish Default MQ Subscribe!")
		startScheduleTrimStream()
		fmt.Println("MQStream Finish Queue Length Cut!")
	}()
	go keepAliveMessageInvokeListener()
}

func innerSettingConsumerName() {
	// Check IP Interfaces
	interfaces, err := net.Interfaces()
	if err != nil {
		fmt.Printf("Error:%s\n", err.Error())

		return
	}

	// range interfaces
	for _, face := range interfaces {
		// skip lo（loopBack）
		if face.Flags&net.FlagLoopback == 0 {
			// Get ALL Addr
			addrList, err := face.Addrs()
			if err != nil {
				fmt.Printf("Error:%s\n", err.Error())

				continue
			}

			// range one
			for _, one := range addrList {
				// change to IPV4
				ip, _, err := net.ParseCIDR(one.String())
				if err != nil {
					fmt.Printf("Error:%s\n", err.Error())

					continue
				}

				// Check IPv4 Addr
				if ip.To4() != nil {
					fmt.Printf("IPv4 Address: %s\n", ip)
					consumerName = ip.String()
				}
			}
		}
	}
}

func createStreamGroup(queueName string, topic string) {
	tryCreateGroup(queueName, topic)
	tryCreateConsumer(queueName)
}

func tryCreateGroup(queueName string, topic string) {
	defer func() {
		if exception := recover(); exception != nil {
			fmt.Printf("MQStream Init TryCreateGroup panic error:%s\n", exception)

			return
		}
	}()

	client := redis.NewClient(GetRedisConfig())
	// Defer Close
	defer func(client *redis.Client) {
		err := client.Close()
		if err != nil {
			fmt.Printf("MQStream sendMessage error:%s\n", err.Error())
		}
	}(client)

	message := &Message{
		Topic: topic,
		Tag:   "blank",
		Body:  "test",
	}
	// Sent Test Stream Message
	_, err := client.XAdd(context.Background(), message.toStreamAddArgsValues(queueName)).Result()
	if err != nil {
		fmt.Printf("MQStream Setup Group Failure Or Group Exsit exception:%s queueName:%s group:%s\n", err, queueName, Group)
	}

	found := false

	groups, _ := client.XInfoGroups(context.Background(), queueName).Result()
	for _, group := range groups {
		if group.Name == Group {
			found = true
		}
	}

	if !found {
		err := client.XGroupCreateMkStream(context.Background(), queueName, Group, "$").Err()
		if err != nil {
			fmt.Printf("MQStream Group exsit queueName:%s groupId:%s err:%s \n", queueName, Group, err.Error())

			return
		} else {
			fmt.Printf("MQStream init queueName:%s groupId:%s \n", queueName, Group)
		}
	}
}

func tryCreateConsumer(queueName string) {
	defer func() {
		if exception := recover(); exception != nil {
			fmt.Printf("MQStream init queue tryCreateConsumer panic error:%s\n", exception)

			return
		}
	}()

	client := redis.NewClient(GetRedisConfig())
	// Close
	defer func(client *redis.Client) {
		err := client.Close()
		if err != nil {
			fmt.Printf("MQStream sendMessage error:%s\n", err.Error())
		}
	}(client)

	if _, err := client.XGroupCreateConsumer(context.Background(), queueName, Group, consumerName).Result(); err != nil {
		fmt.Printf("MQStream consumerName failure or consumerName exsit queueName:%s groupId:%s consumerName:%s err:%s\n", queueName, Group, consumerName, err.Error())
	} else {
		fmt.Printf("MQStream init queueName:%s groupId:%s consumerName:%s\n", queueName, Group, consumerName)
	}
}

func innerLoadConsumer() {
	for _, topic := range Topics {
		blockConsumerTopic(topic)
	}
}

func blockConsumerTopic(topic string) {
	createStreamGroup(GetQueueName(topic), topic)
	createStreamGroup(getBackupQueueName(topic), topic)
	// start background
	go loopConsumer(topic)
	go loopTransactionChecker(topic)
}

func loopConsumer(topic string) {
	client := redis.NewClient(GetRedisConfig())

	defer func(client *redis.Client) {
		err := client.Close()
		if err != nil {
			fmt.Printf("MQStream Closs Redis Stream Client error:%s\n", err.Error())
		}
	}(client)

	for {
		customerIteration(client, topic)
	}
}

func customerIteration(client *redis.Client, topic string) {
	var err error

	defer func() {
		if exception := recover(); exception != nil {
			if v, ok := exception.(error); ok && gerror.HasStack(v) {
				err = v
			} else {
				err = gerror.NewCodef(gcode.CodeInternalPanic, "%+v", exception)
			}

			fmt.Printf("MQStream Stream loopConsumer Redis Error topic:%s panic error:%s\n", topic, err.Error())
		}
	}()

	count := 0

	message := blockReceiveConsumerMessage(client, topic)
	if message != nil {
		if consumer := getConsumer(message); consumer != nil {
			runConsumeMessage(consumer, message)
		} else {
			fmt.Printf("MQStream Stream Receive Group:{} No Comsumer Drop message::%v\n", message)
			messageAck(message)
		}

		count++
	}
	// Sleep
	if count == len(Topics) {
		time.Sleep(1 * time.Second)
	}
}

func loopTransactionChecker(topic string) {
	for {
		loopTransactionCheckerIteration(topic)
	}
}

func loopTransactionCheckerIteration(topic string) {
	defer func() {
		if exception := recover(); exception != nil {
			fmt.Printf("RedisMQ_Query Stream Message Query Transaction Pre Redis Error loopTransactionChecker topic:%s panic error:%s\n", topic, exception)

			return
		}
	}()

	messages := fetchTransactionPrepareMessagesForChecker(topic)
	for _, message := range messages {
		if ck := Checkers()[GetMessageKey(message.Topic, message.Tag)]; ck != nil {
			status := ck.Checker(message)
			switch status {
			case CommitTransaction:
				_, _ = commitTransactionPrepareMessage(message)
			case RollbackTransaction:
				_, _ = rollbackTransactionPrepareMessage(message)
			default:
				// todo mark save send time, max retry times limit 50
				if (CurrentTimeMillis() - message.SendTime) > 1000*60*60*8 {
					// After 8 Hours, Transaction Message Drop To Death
					putMessageToTransactionDeathQueue(topic, message)
				}
			}
		} else {
			if (CurrentTimeMillis() - message.SendTime) > 1000*60*60*24*7 {
				// After 7 Days, Transaction Rollback
				_, _ = rollbackTransactionPrepareMessage(message)
			}
		}

		time.Sleep(1 * time.Second)
	}

	time.Sleep(60 * time.Second)
}

func getConsumer(message *Message) IMessageListener {
	if strings.Compare(message.Tag, "blank") == 0 {
		return nil
	}

	return Listeners()[GetMessageKey(message.Topic, message.Tag)]
}

func runConsumeMessage(consumer IMessageListener, message *Message) {
	var err error

	defer func() {
		if exception := recover(); exception != nil {
			if v, ok := exception.(error); ok && gerror.HasStack(v) {
				err = v
			} else {
				err = gerror.NewCodef(gcode.CodeInternalPanic, "%+v", exception)
			}

			fmt.Printf("RedisMQ Stream Message runConsumeMessage panic error:%s\n", err.Error())

			return
		}
	}()

	if message.isBoardCastingMessage() {
		// todo mark it's a bug
		fmt.Printf("RedisMQ_Receive Stream Message Exception Group Receive Broadcast, Drop messageKey:%s messageId:%v\n", GetMessageKey(message.Topic, message.Tag), message.MessageId)

		return
	}

	cost := CurrentTimeMillis()
	if message.SendTime > 0 {
		cost = CurrentTimeMillis() - message.SendTime
		// history no expire time
		if (CurrentTimeMillis() - message.SendTime) > 1000*60*60*24*3 {
			// message should expire after 3 days, drop
			fmt.Printf("RedisMQ_Receive Stream Message Exception After 3 Days Drop Expired messageKey:%s messageId:%v\n ", GetMessageKey(message.Topic, message.Tag), message.MessageId)

			return
		}
	} else {
		cost = 0
	}

	go func() {
		ctx := context.Background()

		defer func() {
			if exception := recover(); exception != nil {
				// todo mark print exception stack
				fmt.Printf("RedisMQ_Receive Stream Message Error  messageKey:%s messageId:%v panic error:%s\n", GetMessageKey(message.Topic, message.Tag), message.MessageId, exception)

				if pushTaskToResumeLater(message) {
					messageAck(message)
				} else {
					// todo mark enter Resume failure, avoid message loss
				}

				return
			}
		}()

		if message.Topic == TopicInternal && message.Tag == TagInvoke {
			if message.ConsumerDelayMilliSeconds == DefaultConsumerDelayMilliSeconds {
				message.ConsumerDelayMilliSeconds = 20
			}
		}

		if message.ConsumerDelayMilliSeconds > 0 && message.ConsumerDelayMilliSeconds < 10000 {
			time.Sleep(time.Duration(message.ConsumerDelayMilliSeconds) * time.Millisecond)
		} else if message.ConsumerDelayMilliSeconds == 0 {
			time.Sleep(time.Duration(1000) * time.Millisecond)
		}

		action := consumer.Consume(ctx, message)
		fmt.Printf("RedisMQ_Receive Stream Message Consume messageKey:%s result:%d messageId:%v cost:%dms\n", GetMessageKey(message.Topic, message.Tag), action, message.MessageId, cost)

		if action == ReconsumeLater {
			if pushTaskToResumeLater(message) {
				messageAck(message)
			} else {
				// todo mark enter Resume failure, avoid message loss
			}
		} else {
			messageAck(message)
		}
	}()
}

func messageAck(message *Message) {
	var err error

	ctx := context.Background()

	defer func() {
		if exception := recover(); exception != nil {
			if v, ok := exception.(error); ok && gerror.HasStack(v) {
				err = v
			} else {
				err = gerror.NewCodef(gcode.CodeInternalPanic, "%+v", exception)
			}

			g.Log().Errorf(ctx, "MQStream MessageAck panic error:%s\n", err.Error())

			return
		}
	}()

	client := redis.NewClient(GetRedisConfig())

	defer func(client *redis.Client) {
		err := client.Close()
		if err != nil {
			fmt.Printf("MQStream sendMessage error:%s\n", err.Error())
		}
	}(client)

	streamName := GetQueueName(message.Topic)

	ackResult, err := client.XAck(context.Background(), streamName, Group, message.MessageId).Result()
	if err != nil {
		fmt.Printf("MQStream ack message:%v panic error:%s\n", message, err)

		return
	}

	g.Log().Infof(ctx, "MQStream ack streamMessageId:%s streamName:%s ackResult:%d\n", message.MessageId, streamName, ackResult)
}

func blockReceiveConsumerMessage(client *redis.Client, topic string) *Message {
	var err error

	ctx := context.Background()

	defer func() {
		if exception := recover(); exception != nil {
			if v, ok := exception.(error); ok && gerror.HasStack(v) {
				err = v
			} else {
				err = gerror.NewCodef(gcode.CodeInternalPanic, "%+v", exception)
			}

			g.Log().Errorf(ctx, "MQStream blockReceiveConsumerMessage topic:%s panic error:%v %v\n", topic, err.Error(), exception)

			return
		}
	}()

	streamName := GetQueueName(topic)

	result, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    Group,
		Consumer: consumerName,
		Streams:  []string{streamName, ">"},
		Count:    1,
		Block:    60 * time.Second,
		NoAck:    true,
	}).Result()
	if err != nil {
		g.Log().Debugf(ctx, "MQStream blockReceiveConsumerMessage streamName=%s err=%s\n", streamName, err.Error())

		return nil
	}

	if len(result) == 1 && len(result[0].Messages) == 1 {
		messageId := result[0].Messages[0].ID
		value := result[0].Messages[0].Values
		message := Message{}
		message.MessageId = messageId
		message.getUniqueKey()
		message.passStreamMessage(value)

		return &message
	}

	return nil
}

func pushTaskToResumeLater(message *Message) bool {
	ResumeTimesMax := MaxInt(40, message.ReconsumeMax)
	fmt.Printf("RedisMq_pushTaskToResumeLater messageId:%s, topic:%s tag:%s ResumeTimesMax:%v/%v \n", message.MessageId, message.Topic, message.Tag, message.ReconsumeTimes, ResumeTimesMax)

	if message.ReconsumeTimes >= ResumeTimesMax {
		return putMessageToDeathQueue(message)
	} else {
		message.ReconsumeTimes = message.ReconsumeTimes + 1

		var appendTime = MaxInt64(60, int64(60*message.ReconsumeTimes))

		message.StartDeliverTime = gtime.Now().Timestamp() + appendTime // resume every min till end

		return sendDelayMessage(message)
	}
}

func putMessageToDeathQueue(message *Message) bool {
	client := redis.NewClient(GetRedisConfig())
	defer func(client *redis.Client) {
		err := client.Close()
		if err != nil {
			fmt.Printf("sendMessage error:%s\n", err)
		}
	}(client)

	streamMessageId, err := client.XAdd(context.Background(), message.toStreamAddArgsValues(GetDeathQueueName())).Result()
	if err != nil {
		fmt.Printf("MQStream push message to death error:%s messageId:%s", err.Error(), message.MessageId)

		return false
	}

	fmt.Printf("MQStream push message to death, messageId=%s deathMessageId:%s", message.MessageId, streamMessageId)

	return true
}

func putMessageToTransactionDeathQueue(topic string, message *Message) bool {
	client := redis.NewClient(GetRedisConfig())
	defer func(client *redis.Client) {
		err := client.Close()
		if err != nil {
			fmt.Printf("MQStream push transaction message to death error:%s\n", err.Error())
		}
	}(client)

	_, err := client.TxPipelined(context.Background(), func(pipe redis.Pipeliner) error {
		pipe.LRem(context.Background(), GetTransactionPrepareQueueName(topic), 1, message.MessageId)
		pipe.RPush(context.Background(), getTransactionDeathQueueName(), message.MessageId)

		return nil
	})
	if err != nil {
		fmt.Printf("MQStream transaction message to death and delete exception:%s message:%v\n", err, message)

		return false
	}

	return true
}

func fetchTransactionPrepareMessagesForChecker(topic string) []*Message {
	client := redis.NewClient(GetRedisConfig())
	defer func(client *redis.Client) {
		err := client.Close()
		if err != nil {
			fmt.Printf("MQ redis error:%s\n", err.Error())
		}
	}(client)

	result, err := client.LRange(context.Background(), GetTransactionPrepareQueueName(topic), 0, -1).Result()
	if err != nil {
		return []*Message{}
	}

	var messages = make([]*Message, 0)

	for _, messageId := range result {
		if len(messageId) > 0 {
			value, _ := client.Get(context.Background(), messageId).Result()
			if len(value) > 0 {
				var message *Message

				err = gjson.Unmarshal([]byte(value), &message)
				if err == nil {
					messages = append(messages, message)
				}
			} else {
				fmt.Printf("MQStream transaction pre message messageId:%s\n", messageId)
			}
		}
	}

	return messages
}

func startScheduleTrimStream() {
	go func() {
		client := redis.NewClient(GetRedisConfig())
		defer func(client *redis.Client) {
			err := client.Close()
			if err != nil {
				fmt.Printf("MQStream redis error:%s\n", err.Error())
			}
		}(client)

		for {
			startScheduleTrimStreamIteration(client)

			time.Sleep(1000 * 60 * 10 * time.Second)
		}
	}()
}

func startScheduleTrimStreamIteration(client *redis.Client) {
	const maxLen = 10000

	defer func() {
		if exception := recover(); exception != nil {
			fmt.Printf("MQStream startScheduleTrimStream exception:%s\n", exception)

			return
		}
	}()

	for _, topic := range Topics {
		queueName := GetQueueName(topic)
		client.XTrimMaxLen(context.Background(), queueName, int64(maxLen))
		queueName = getBackupQueueName(topic)
		client.XTrimMaxLen(context.Background(), queueName, int64(maxLen))
	}

	queueName := GetDeathQueueName()
	client.XTrimMaxLen(context.Background(), queueName, int64(maxLen))
}
