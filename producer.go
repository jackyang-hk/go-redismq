package go_redismq

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/gogf/gf/v2/os/gtime"
	"github.com/redis/go-redis/v9"
)

func Send(message *Message) (bool, error) {
	return sendMessage(message, "ProducerWrapper")
}

func SendTransaction(message *Message, transactionExecuter func(messageToSend *Message) (TransactionStatus, error)) (bool, error) {
	if strings.Compare(message.Tag, "blank") == 0 {
		return false, errors.New("blank tag message")
	}

	if message.StartDeliverTime > 0 {
		return false, errors.New("delay message not support transaction")
	}

	send, err := sendTransactionPrepareMessage(message)
	if err != nil || !send {
		return send, err
	}

	status, err := transactionExecuter(message)
	switch status {
	case RollbackTransaction:
		_, rollBackErr := rollbackTransactionPrepareMessage(message)
		if rollBackErr != nil {
			fmt.Printf("rollbackTransactionPrepareMessage err:%s rollBackError:%s\n", err, rollBackErr)
		}

		return false, err
	case CommitTransaction:
		return commitTransactionPrepareMessage(message)
	default:
		return false, errors.New("unknown transaction status")
	}
}

func sendDelayMessage(message *Message) bool {
	Assert(message.StartDeliverTime-gtime.Now().Timestamp() > 0, "StartDeliverTime Invalid, should > now")
	send, err := SendDelay(message, message.StartDeliverTime-gtime.Now().Timestamp())
	fmt.Printf("Redismq SendDelayMessage result:%v", send)

	if err != nil {
		return false
	}

	return send
}

func sendMessage(message *Message, source string) (bool, error) {
	if strings.Compare(message.Tag, "blank") == 0 {
		return false, errors.New("blank空消息")
	}

	message.SendTime = CurrentTimeMillis()
	Assert(len(message.MessageId) == 0, "Send Stream Need Blank MessageId")

	client := redis.NewClient(GetRedisConfig())

	defer func(client *redis.Client) {
		err := client.Close()
		if err != nil {
			fmt.Printf("sendMessage error:%s\n", err)
		}
	}(client)

	streamMessageId, err := client.XAdd(context.Background(), message.toStreamAddArgsValues(GetQueueName(message.Topic))).Result()
	if err != nil {
		return false, fmt.Errorf("RedisMQ_Send Stream Message exception:%w queueName=%s message:%v\n", err, GetQueueName(message.Topic), MarshalToJsonString(message))
	}

	message.MessageId = streamMessageId
	fmt.Printf("RedisMQ_Send Stream Message Success Source:%s QueueName=%s messageKey:%s MessageId=%v\n", source, GetQueueName(message.Topic), GetMessageKey(message.Topic, message.Tag), message.MessageId)

	return true, nil
}

func sendTransactionPrepareMessage(message *Message) (bool, error) {
	if strings.Compare(message.Tag, "blank") == 0 {
		return false, errors.New("Blank Message")
	}

	message.MessageId = GenerateUniqueNo(message.Topic)
	message.SendTime = CurrentTimeMillis()
	client := redis.NewClient(GetRedisConfig())

	defer func(client *redis.Client) {
		err := client.Close()
		if err != nil {
			fmt.Printf("sendTransactionPrepareMessage error:%s\n", err)
		}
	}(client)

	messageJson, err := gjson.Marshal(message)

	jsonString := string(messageJson)

	if err != nil {
		return false, fmt.Errorf("Send MQ Transaction Pre exception:%s message:%v\n", err.Error(), message)
	}
	// 执行事务
	_, err = client.TxPipelined(context.Background(), func(pipe redis.Pipeliner) error {
		pipe.Set(context.Background(), message.MessageId, jsonString, -1)
		pipe.LPush(context.Background(), GetTransactionPrepareQueueName(message.Topic), message.MessageId)

		return nil
	})
	if err != nil {
		return false, fmt.Errorf("Send MQ Transaction Pre  exception:%s message:%v\n", err.Error(), message)
	}

	return true, nil
}

func rollbackTransactionPrepareMessage(message *Message) (bool, error) {
	return delTransactionPrepareMessage(message)
}

func delTransactionPrepareMessage(message *Message) (bool, error) {
	client := redis.NewClient(GetRedisConfig())

	defer func(client *redis.Client) {
		err := client.Close()
		if err != nil {
			fmt.Printf("delTransactionPrepareMessage error:%s\n", err)
		}
	}(client)

	_, err := client.TxPipelined(context.Background(), func(pipe redis.Pipeliner) error {
		pipe.Del(context.Background(), message.MessageId)
		pipe.LRem(context.Background(), GetTransactionPrepareQueueName(message.Topic), 1, message.MessageId)

		return nil
	})
	if err != nil {
		return false, fmt.Errorf("Del MQ Transaction Pre  exception:%w message:%v\n", err, message)
	}

	fmt.Printf("rollbackTransactionPrepareMessage message:%v\n", message)

	return true, nil
}

func commitTransactionPrepareMessage(message *Message) (bool, error) {
	oldMessageId := message.MessageId
	message.MessageId = ""
	client := redis.NewClient(GetRedisConfig())

	defer func(client *redis.Client) {
		err := client.Close()
		if err != nil {
			fmt.Printf("commmitTransactionPrepareMessage error:%s\n", err)
		}
	}(client)

	streamMessageId := ""

	_, err := client.TxPipelined(context.Background(), func(pipe redis.Pipeliner) error {
		streamMessageId, _ = client.XAdd(context.Background(), message.toStreamAddArgsValues(GetQueueName(message.Topic))).Result()
		message.MessageId = streamMessageId

		pipe.Del(context.Background(), oldMessageId)
		pipe.LRem(context.Background(), GetTransactionPrepareQueueName(message.Topic), 1, oldMessageId)

		return nil
	})
	if err != nil {
		return false, fmt.Errorf("Commit MQ Transaction Pre  exception:%w message:%v\n", err, message)
	}

	fmt.Printf("Redismq commitTransactionPrepareMessage success message:%v prepareMessageId:%s targetMessageId:%s ", message, oldMessageId, streamMessageId)

	return true, nil
}
