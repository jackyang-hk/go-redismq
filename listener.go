package go_redismq

import (
	"context"
	"strings"
)

type IMessageListener interface {
	GetTopic() string
	GetTag() string
	Consume(ctx context.Context, message *Message) Action
}

var listeners map[string]IMessageListener
var Topics []string

func Listeners() map[string]IMessageListener {
	if listeners == nil {
		listeners = make(map[string]IMessageListener)
	}

	return listeners
}

func isValidTopic(topic string) bool {
	return len(topic) > 0 && strings.Compare(topic, "*") != 0
}

func RegisterListener(i IMessageListener) {
	if i == nil {
		return
	}

	if Topics == nil {
		Topics = make([]string, 0, 100)
	}

	if len(Topics) > 60 {
		logger.Warnf("Project Register Topic Too Much , Merge Please")

		return
	}

	if !isValidTopic(i.GetTopic()) {
		logger.Warnf("Redismq Regist Default Consumer Invalid Topic:%s,Drop", i.GetTopic())

		return
	}

	if Listeners()[GetMessageKey(i.GetTopic(), i.GetTag())] != nil {
		logger.Warnf("Redismq Multi %s,Consumer On:%s,Drop", i, GetMessageKey(i.GetTopic(), i.GetTag()))
	} else {
		messageKey := GetMessageKey(i.GetTopic(), i.GetTag())
		Listeners()[messageKey] = i
		Topics = append(Topics, i.GetTopic())
		logger.Infof("Redismq Register IMessageListener:%s,Consumer:%s", i, messageKey)
	}
}
