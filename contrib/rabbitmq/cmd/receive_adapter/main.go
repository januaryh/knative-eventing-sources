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

package main

import (
	"context"
	"flag"
	"github.com/knative/eventing-sources/contrib/rabbitmq/pkg/adapter"
	"github.com/knative/pkg/signals"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"log"
	"os"
	"strconv"
)

const (
	envBrokers             = "RABBITMQ_BROKERS"
	envTopic               = "RABBITMQ_TOPIC"
	envRoutingKey          = "RABBITMQ_ROUTING_KEY"
        envExchangeName        = "RABBITMQ_EXCHANGE_CONFIG_NAME"
	envExchangeType        = "RABBITMQ_EXCHANGE_CONFIG_TYPE"
	envExchangeDurable     = "RABBITMQ_EXCHANGE_CONFIG_DURABLE"
	envExchangeAutoDeleted = "RABBITMQ_EXCHANGE_CONFIG_AUTO_DELETED"
	envExchangeInternal    = "RABBITMQ_EXCHANGE_CONFIG_INTERNAL"
	envExchangeNoWait      = "RABBITMQ_EXCHANGE_CONFIG_NOWAIT"
	envQueueName           = "RABBITMQ_QUEUE_CONFIG_NAME"
	envQueueDurable        = "RABBITMQ_QUEUE_CONFIG_DURABLE"
	envQueueAutoDeleted    = "RABBITMQ_QUEUE_CONFIG_AUTO_DELETED"
	envQueueExclusive      = "RABBITMQ_QUEUE_CONFIG_EXCLUSIVE"
	envQueueNoWait         = "RABBITMQ_QUEUE_CONFIG_NOWAIT"
	envSinkURI             = "SINK_URI"
)

func getRequiredEnv(key string) string {
	val, defined := os.LookupEnv(key)
	if !defined {
		log.Fatalf("Required environment variable not defined for key '%s'.", key)
	}

	return val
}

func getOptionalBoolEnv(key string) bool {
	if val, defined := os.LookupEnv(key); defined {
		if res, err := strconv.ParseBool(val); err != nil {
			log.Fatalf("A value of '%s' cannot be parsed as a boolean value.", val)
		} else {
			return res
		}
	}

	return false
}

func getOptionalEnv(key string) string {
	if val, defined := os.LookupEnv(key); defined {
		return val
	}
	return ""
}

func main() {
	flag.Parse()

	ctx := context.Background()
	logCfg := zap.NewProductionConfig()
	logCfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	logger, err := logCfg.Build()
	if err != nil {
		log.Fatalf("Unable to create logger: %v", err)
	}

	adapter := &rabbitmq.Adapter{
		Brokers: getRequiredEnv(envBrokers),
		Topic:   getRequiredEnv(envTopic),
		ExchangeConfig: rabbitmq.ExchangeConfig{
                        Name:        getOptionalEnv(envExchangeName),
			TypeOf:      getRequiredEnv(envExchangeType),
			Durable:     getOptionalBoolEnv(envExchangeDurable),
			AutoDeleted: getOptionalBoolEnv(envExchangeAutoDeleted),
			Internal:    getOptionalBoolEnv(envExchangeInternal),
			NoWait:      getOptionalBoolEnv(envExchangeNoWait),
		},
		QueueConfig: rabbitmq.QueueConfig{
			Name:             getOptionalEnv(envQueueName),
			RoutingKey:       getRequiredEnv(envRoutingKey),
			Durable:          getOptionalBoolEnv(envQueueDurable),
			DeleteWhenUnused: getOptionalBoolEnv(envQueueAutoDeleted),
			Exclusive:        getOptionalBoolEnv(envQueueExclusive),
			NoWait:           getOptionalBoolEnv(envQueueNoWait),
		},
		SinkURI: getRequiredEnv(envSinkURI),
	}

	stopCh := signals.SetupSignalHandler()

	logger.Info("Starting Rabbitmq Receive Adapter...", zap.Reflect("adapter", adapter))
	if err := adapter.Start(ctx, stopCh); err != nil {
		logger.Fatal("failed to start adapter: ", zap.Error(err))
	}
}
