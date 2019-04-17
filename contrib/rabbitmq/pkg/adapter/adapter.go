/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/cloudevents/sdk-go/pkg/cloudevents"
	"github.com/cloudevents/sdk-go/pkg/cloudevents/client"
	"github.com/cloudevents/sdk-go/pkg/cloudevents/types"
	"github.com/knative/eventing-sources/pkg/kncloudevents"
	"github.com/knative/pkg/logging"
	"github.com/sbcd90/wabbit"
	"github.com/sbcd90/wabbit/amqp"
	"go.uber.org/zap"
	"strings"
)

const (
	eventType = "dev.knative.rabbitmq.event"
)

type ExchangeConfig struct {
	Name        string
	TypeOf      string
	Durable     bool
	AutoDeleted bool
	Internal    bool
	NoWait      bool
}

type ChannelConfig struct {
	PrefetchCount int
	GlobalQos     bool
}

type QueueConfig struct {
	Name             string
	RoutingKey       string
	Durable          bool
	DeleteWhenUnused bool
	Exclusive        bool
	NoWait           bool
}

type Adapter struct {
	Brokers        string
	Topic          string
	User           string
	Password       string
	ChannelConfig  ChannelConfig
	ExchangeConfig ExchangeConfig
	QueueConfig    QueueConfig
	SinkURI        string
	client         client.Client
}

func (a *Adapter) initClient() error {
	if a.client == nil {
		var err error
		if a.client, err = kncloudevents.NewDefaultClient(a.SinkURI); err != nil {
			return err
		}
	}
	return nil
}

func (a *Adapter) Start(ctx context.Context, stopCh <-chan struct{}) error {
	logger := logging.FromContext(ctx)

	if err := a.initClient(); err != nil {
		logger.Error("Failed to create cloudevent client", zap.Error(err))
		return err
	}

	logger.Info("Starting with config: ", zap.Any("adapter", a))

	if a.User != "" && a.Password != "" {
		a.Brokers = fmt.Sprintf("amqp://%s:%s@%s", a.User, a.Password, a.Brokers)
	}
	conn, err := amqp.Dial(a.Brokers)
	if err != nil {
		logger.Error(err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		logger.Error(err)
	}
	defer ch.Close()

	err = ch.Qos(a.ChannelConfig.PrefetchCount,
		0,
		a.ChannelConfig.GlobalQos)
	if err != nil {
		logger.Error(err)
	}

	queue, err := a.StartAmqpClient(ctx, &ch)
	if err != nil {
		logger.Error(err)
	}

	return a.pollForMessages(ctx, &ch, queue, stopCh)
}

func (a *Adapter) StartAmqpClient(ctx context.Context, ch *wabbit.Channel) (*wabbit.Queue, error) {
	logger := logging.FromContext(ctx)
	exchangeConfig := fillDefaultValuesForExchangeConfig(&a.ExchangeConfig, a.Topic)

	err := (*ch).ExchangeDeclare(
		exchangeConfig.Name,
		exchangeConfig.TypeOf,
		wabbit.Option{
			"durable":  exchangeConfig.Durable,
			"delete":   exchangeConfig.AutoDeleted,
			"internal": exchangeConfig.Internal,
			"noWait":   exchangeConfig.NoWait,
		},)
	if err != nil {
		logger.Error(err)
		return nil, err
	}

	queue, err := (*ch).QueueDeclare(
		a.QueueConfig.Name,
		wabbit.Option{
			"durable":   a.QueueConfig.Durable,
			"delete":    a.QueueConfig.DeleteWhenUnused,
			"exclusive": a.QueueConfig.Exclusive,
			"noWait":    a.QueueConfig.NoWait,
		},)
	if err != nil {
		logger.Error(err)
		return nil, err
	}

	if a.ExchangeConfig.TypeOf != "fanout" {
		routingKeys := strings.Split(a.QueueConfig.RoutingKey, ",")

		for _, routingKey := range routingKeys {
			err = (*ch).QueueBind(
				queue.Name(),
				routingKey,
				exchangeConfig.Name,
				wabbit.Option{
					"noWait": a.QueueConfig.NoWait,
				},)
			if err != nil {
				logger.Error(err)
				return nil, err
			}
		}
	} else {
		err = (*ch).QueueBind(
			queue.Name(),
			"",
			exchangeConfig.Name,
			wabbit.Option{
				"noWait": a.QueueConfig.NoWait,
			})
		if err != nil {
			logger.Error(err)
			return nil, err
		}
	}

	return &queue, nil
}

func (a *Adapter) pollForMessages(ctx context.Context, channel *wabbit.Channel,
	queue *wabbit.Queue, stopCh <-chan struct{}) error {
	logger := logging.FromContext(ctx)

	msgs, err := (*channel).Consume(
		(*queue).Name(),
		"",
		wabbit.Option{
			"noAck":     false,
			"exclusive": a.QueueConfig.Exclusive,
			"noLocal":   false,
			"noWait":    a.QueueConfig.NoWait,
		},)

	if err != nil {
		logger.Error(err)
	}

	for {
		select {
		case msg, ok := <-msgs:
			if ok {
				logger.Info("Received: ", zap.Any("value", string(msg.Body())))

				go func(message *wabbit.Delivery) {
					if err := a.postMessage(ctx, *message); err == nil {
						logger.Info("Successfully sent event to sink")
						err = (*channel).Ack((*message).DeliveryTag(), true)
						if err != nil {
							logger.Error("Sending Ack failed with Delivery Tag")
						}
					} else {
						logger.Error("Sending event to sink failed: ", zap.Error(err))
						err = (*channel).Nack((*message).DeliveryTag(), true, true)
						if err != nil {
							logger.Error("Sending Nack failed with Delivery Tag")
						}
					}
				}(&msg)
			} else {
				return nil
			}
		case <-stopCh:
			logger.Info("Shutting down...")
			return nil
		}
	}
}

func (a *Adapter) postMessage(ctx context.Context, msg wabbit.Delivery) error {

	extensions := map[string]interface{}{
		"key": string(msg.MessageId()),
	}
	event := cloudevents.Event{
		Context: cloudevents.EventContextV02{
			SpecVersion: cloudevents.CloudEventsVersionV02,
			Type:        eventType,
			ID:          msg.MessageId(),
			Time:        &types.Timestamp{msg.Timestamp()},
			Source:      *types.ParseURLRef(a.Brokers),
			ContentType: cloudevents.StringOfApplicationJSON(),
			Extensions:  extensions,
		}.AsV02(),
		Data: a.jsonEncode(ctx, msg.Body()),
	}

	if _, err := a.client.Send(ctx, event); err != nil {
		return err
	} else {
		return nil
	}
}

func (a *Adapter) jsonEncode(ctx context.Context, body []byte) interface{} {
	var payload map[string]interface{}

	logger := logging.FromContext(ctx)

	if err := json.Unmarshal(body, &payload); err != nil {
		logger.Info("Error unmarshalling JSON: ", zap.Error(err))
		return body
	} else {
		return payload
	}
}

func fillDefaultValuesForExchangeConfig(config *ExchangeConfig, topic string) *ExchangeConfig {
	if config.TypeOf != "topic" {
		if config.Name == "" {
			config.Name = "logs"
		}
	} else {
		config.Name = topic
	}
	return config
}
