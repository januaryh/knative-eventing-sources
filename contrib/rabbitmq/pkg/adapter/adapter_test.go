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
	"github.com/sbcd90/wabbit/amqp"
	origamqp "github.com/streadway/amqp"
	"github.com/sbcd90/wabbit/amqptest"
	"github.com/sbcd90/wabbit/amqptest/server"
	"github.com/cloudevents/sdk-go/pkg/cloudevents/client"
	"github.com/knative/eventing-sources/pkg/kncloudevents"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPostMessage_ServeHttp(t *testing.T) {
	testCases := map[string]struct{
		sink              func(http.ResponseWriter, *http.Request)
		reqBody           string
		attributes        map[string]string
		expectedEventType string
		error             bool
	}{
		"accepted": {
			sink:    sinkAccepted,
			reqBody: `{"key":"value"}`,
		},
		"rejected": {
			sink:    sinkRejected,
			reqBody: `{"key":"value"}`,
			error:   true,
		},
	}

	for n, tc := range testCases {
		t.Run(n, func(t *testing.T) {
			h := &fakeHandler{
				handler: tc.sink,
			}
			sinkServer := httptest.NewServer(h)
			defer sinkServer.Close()

			a := &Adapter{
				Topic:          "topic",
				Brokers:        "amqp://guest:guest@localhost:5672/",
				ExchangeConfig: ExchangeConfig{
					TypeOf:      "topic",
					Durable:     true,
					AutoDeleted: false,
					Internal:    false,
					NoWait:      false,
				},
				QueueConfig:   QueueConfig{
					Name:             "",
					Durable:          false,
					DeleteWhenUnused: false,
					Exclusive:        true,
					NoWait:           false,
				},
				SinkURI:       sinkServer.URL,
				client: func() client.Client {
					c, _ := kncloudevents.NewDefaultClient(sinkServer.URL)
					return c
				}(),
			}

			data, err := json.Marshal(map[string]string{"key": "value"})
			if err != nil {
				t.Errorf("unexpected error, %v", err)
			}

			m := &amqp.Delivery{}
			m.Delivery = &origamqp.Delivery{
				MessageId: "id",
				Body:      []byte(data),
			}
			err = a.postMessage(context.TODO(), m)

			if tc.error && err == nil {
				t.Errorf("expected error, but got %v", err)
			}

			et := h.header.Get("Ce-Type")

			expectedEventType := eventType
			if tc.expectedEventType != "" {
				expectedEventType = tc.expectedEventType
			}

			if et != expectedEventType {
				t.Errorf("Expected eventtype '%q', but got '%q'", tc.expectedEventType, et)
			}
			if tc.reqBody != string(h.body) {
				t.Errorf("Expected request body '%q', but got '%q'", tc.reqBody, h.body)
			}
		})
	}
}

func TestAdapter_StartAmqpClient(t *testing.T) {
	fakeServer := server.NewServer("amqp://localhost:5672/%2f")
	err := fakeServer.Start()
	if err != nil {
		t.Errorf("%s: %s", "Failed to connect to RabbitMQ", err)
	}

	conn, err := amqptest.Dial("amqp://localhost:5672/%2f")
	if err != nil {
		t.Errorf("%s: %s", "Failed to connect to RabbitMQ", err)
	}

	channel, err := conn.Channel()
	if err != nil {
		t.Errorf("Failed to open a channel")
	}

	a := &Adapter{
		Topic:          "",
		Brokers:        "amqp://localhost:5672/%2f",
		ExchangeConfig: ExchangeConfig{
			TypeOf:      "direct",
			Durable:     true,
			AutoDeleted: false,
			Internal:    false,
			NoWait:      false,
		},
		QueueConfig:   QueueConfig{
			Name:             "",
			Durable:          false,
			DeleteWhenUnused: false,
			Exclusive:        true,
			NoWait:           false,
		},
	}
	_, err = a.StartAmqpClient(context.TODO(), &channel)
	if err != nil {
		t.Errorf("Failed to start RabbitMQ")
	}
}

type fakeHandler struct {
	body   []byte
	header http.Header

	handler func(http.ResponseWriter, *http.Request)
}

func (h *fakeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request)  {
	h.header = r.Header
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "can not read body", http.StatusBadRequest)
		return
	}
	h.body = body

	defer r.Body.Close()
	h.handler(w, r)
}

func sinkAccepted(writer http.ResponseWriter, req *http.Request) {
	writer.WriteHeader(http.StatusOK)
}

func sinkRejected(writer http.ResponseWriter, _ *http.Request) {
	writer.WriteHeader(http.StatusRequestTimeout)
}
