// Copyright 2016 Canonical Ltd.
// Licensed under the LGPLv3, see LICENCE file for details.

package pubsub

import (
	"encoding/json"
	"reflect"

	"github.com/juju/errors"
	"github.com/juju/loggo"
)

type structuredHub struct {
	simplehub

	annotations map[string]interface{}
}

// NewStructuredHub returns a new Hub instance.
//
// A structured hub serializes the data through an intermediate format.
// In this case, JSON.
// The annotations are added to each message that is published IFF the values
// are not already set.
func NewStructuredHub(annotations map[string]interface{}) Hub {
	return &structuredHub{
		simplehub: simplehub{
			logger: loggo.GetLogger("pubsub.structured"),
		},
		annotations: annotations,
	}
}

// Publish implements Hub.
func (h *structuredHub) Publish(topic string, data interface{}) (Completer, error) {
	asMap, err := h.toStringMap(data)
	if err != nil {
		return nil, errors.Trace(err)
	}
	for key, value := range h.annotations {
		if _, exists := asMap[key]; !exists {
			asMap[key] = value
		}
	}
	return h.simplehub.Publish(topic, asMap)
}

func (h *structuredHub) toStringMap(data interface{}) (map[string]interface{}, error) {
	var result map[string]interface{}
	resultType := reflect.TypeOf(result)
	dataType := reflect.TypeOf(data)
	if dataType.AssignableTo(resultType) {
		cast, ok := data.(map[string]interface{})
		if !ok {
			return nil, errors.Errorf("%T assignable to map[string]interface{} but isn't one?", data)
		}
		return cast, nil
	}
	bytes, err := json.Marshal(data)
	if err != nil {
		return nil, errors.Annotate(err, "json marshalling")
	}
	err = json.Unmarshal(bytes, &result)
	if err != nil {
		return nil, errors.Annotate(err, "json unmarshalling")
	}
	return result, nil
}

// Subscribe implements Hub.
func (h *structuredHub) Subscribe(topic string, handler interface{}) (Unsubscriber, error) {
	rt, err := h.checkHandler(handler)
	if err != nil {
		return nil, errors.Trace(err)
	}
	f := reflect.ValueOf(handler)
	// Wrap the hander func in something that deserializes the YAML into the structure expected.
	deserialize := func(t string, data interface{}) {
		var (
			err   error
			value reflect.Value
		)
		asMap, ok := data.(map[string]interface{})
		if !ok {
			err = errors.Errorf("bad publish data: %v", data)
			value = reflect.Indirect(reflect.New(rt))
		} else {
			value, err = h.toHanderType(rt, asMap)
		}
		// NOTE: you can't just use reflect.ValueOf(err) as that doesn't work
		// with nil errors. reflect.ValueOf(nil) isn't a valid value. So we need
		// to make  sure that we get the type of the parameter correct, which is
		// the error interface.
		errValue := reflect.Indirect(reflect.ValueOf(&err))
		args := []reflect.Value{reflect.ValueOf(t), value, errValue}
		f.Call(args)
	}
	return h.simplehub.Subscribe(topic, deserialize)
}

func (h *structuredHub) toHanderType(rt reflect.Type, data map[string]interface{}) (reflect.Value, error) {
	mapType := reflect.TypeOf(data)
	if mapType == rt {
		return reflect.ValueOf(data), nil
	}
	sv := reflect.New(rt) // returns a Value containing *StructType
	bytes, err := json.Marshal(data)
	if err != nil {
		return reflect.Indirect(sv), errors.Annotate(err, "json marshalling")
	}
	err = json.Unmarshal(bytes, sv.Interface())
	if err != nil {
		return reflect.Indirect(sv), errors.Annotate(err, "json unmarshalling")
	}
	return reflect.Indirect(sv), nil
}

// checkHandler makes sure that the handler is a function that takes a string and
// a structure. Returns the reflect.Type for the structure.
func (h *structuredHub) checkHandler(handler interface{}) (reflect.Type, error) {
	mapType := reflect.TypeOf(map[string]interface{}{})
	t := reflect.TypeOf(handler)
	if t.Kind() != reflect.Func {
		return nil, errors.NotValidf("handler of type %T", handler)
	}
	if t.NumIn() != 3 || t.NumOut() != 0 {
		return nil, errors.NotValidf("incorrect handler signature")
	}
	arg1 := t.In(0)
	arg2 := t.In(1)
	arg3 := t.In(2)
	if arg1.Kind() != reflect.String {
		return nil, errors.NotValidf("incorrect handler signature, first arg should be a string for topic")
	}
	if arg2.Kind() != reflect.Struct && arg2 != mapType {
		return nil, errors.NotValidf("incorrect handler signature, second arg should be a structure for data")
	}
	if arg3.Kind() != reflect.Interface || arg3.Name() != "error" {
		h.logger.Errorf("expected error type, got %#v", arg3.Name())
		return nil, errors.NotValidf("incorrect handler signature, third arg should error for deserialization errors")
	}
	return arg2, nil
}
