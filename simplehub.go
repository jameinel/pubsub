// Copyright 2016 Canonical Ltd.
// Licensed under the LGPLv3, see LICENCE file for details.

package pubsub

import (
	"sync"

	"github.com/juju/errors"
	"github.com/juju/loggo"
)

// NewSimpleHub returns a new Hub instance.
//
// A simple hub does not touch the data that is passed through to Publish.
// This data is passed through to each Subscriber. Note that all subscribers
// are notified in parallel, and that no modification should be done to the
// data or data races will occur.
func NewSimpleHub() Hub {
	return &simplehub{
		logger: loggo.GetLogger("pubsub.simple"),
	}
}

type simplehub struct {
	mutex       sync.Mutex
	subscribers []*subscriber
	idx         int
	logger      loggo.Logger
}

type doneHandle struct {
	done chan struct{}
}

func (d *doneHandle) Complete() <-chan struct{} {
	return d.done
}

func (h *simplehub) dupeSubscribers() []*subscriber {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	dupe := make([]*subscriber, len(h.subscribers))
	copy(dupe, h.subscribers)
	return dupe
}

func (s *subscriber) matchTopic(topic string) bool {
	return s.topic.MatchString(topic)
}

func (h *simplehub) Publish(topic string, data interface{}) (Completer, error) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	done := make(chan struct{})
	wait := sync.WaitGroup{}

	for _, s := range h.subscribers {
		if s.matchTopic(topic) {
			wait.Add(1)
			s.notify(func() {
				defer wait.Done()
				s.handler(topic, data)
			})
		}
	}

	go func() {
		wait.Wait()
		close(done)
	}()

	return &doneHandle{done: done}, nil
}

func (h *simplehub) Subscribe(topic string, handler interface{}) (Unsubscriber, error) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	sub, err := newSubscriber(topic, handler)
	if err != nil {
		return nil, errors.Trace(err)
	}

	sub.id = h.idx
	h.idx++
	h.subscribers = append(h.subscribers, sub)
	return &handle{hub: h, id: sub.id}, nil
}

func (h *simplehub) unsubscribe(id int) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	for i, sub := range h.subscribers {
		if sub.id == id {
			sub.close()
			h.subscribers = append(h.subscribers[0:i], h.subscribers[i+1:]...)
			return
		}
	}
}

type handle struct {
	hub *simplehub
	id  int
}

func (h *handle) Unsubscribe() {
	h.hub.unsubscribe(h.id)
}
