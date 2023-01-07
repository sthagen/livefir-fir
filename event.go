package fir

import (
	"bytes"
	"encoding/json"
	"html/template"
	"io"
	"strings"

	"github.com/golang/glog"
	"github.com/livefir/fir/pubsub"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/html"
)

// NewEvent creates a new event
func NewEvent(id string, params any) Event {
	data, err := json.Marshal(params)
	if err != nil {
		glog.Errorf("error marshaling event params: %v, %v, %v \n,", id, params, err)
		return Event{
			ID: id,
		}
	}
	return Event{
		ID:     id,
		Params: data,
	}
}

// Event is a struct that holds the data for an incoming event
type Event struct {
	// Name is the name of the event
	ID string `json:"event_id"`
	// Params is the json rawmessage to be passed to the event
	Params   json.RawMessage `json:"params"`
	Target   *string         `json:"target,omitempty"`
	Redirect bool            `json:"redirect,omitempty"`
	IsForm   bool            `json:"is_form,omitempty"`
	RouteID  *string         `json:"route_id,omitempty"`
}

// String returns the string representation of the event
func (e Event) String() string {
	data, _ := json.MarshalIndent(e, "", " ")
	return string(data)
}

func fir(parts ...string) *string {
	parts = append([]string{"fir"}, parts...)
	s := strings.Join(parts, ":")
	return &s
}

type DOMEvent struct {
	Type   *string `json:"type,omitempty"`
	Target *string `json:"target,omitempty"`
	Detail any     `json:"detail,omitempty"`
}

// domEvents converts pubsub events to dom events and returns the json representation
func domEvents(t *template.Template, pubsubEvents []pubsub.Event) []byte {
	var events []DOMEvent
	for _, e := range pubsubEvents {
		if e.TemplateName == nil {
			events = append(events, DOMEvent{
				Type:   e.Type,
				Target: e.Target,
				Detail: e.Data,
			})
			continue
		}

		detail, err := buildTemplateValue(t, *e.TemplateName, e.Data)
		if err != nil {
			glog.Errorf("[warning]event buildTemplateValue error: %v,%+v \n", err, e)
			continue
		}

		events = append(events, DOMEvent{
			Type:   e.Type,
			Target: e.Target,
			Detail: detail,
		})
	}

	data, err := json.Marshal(events)
	if err != nil {
		glog.Errorf("dom events marshal error: %+v, %v \n", events, err)
		return nil
	}
	return data
}

func buildTemplateValue(t *template.Template, name string, data any) (string, error) {
	var buf bytes.Buffer
	defer buf.Reset()
	if name == "_fir_html" {
		buf.WriteString(data.(string))
	} else {
		t.Option("missingkey=zero")
		err := t.ExecuteTemplate(&buf, name, data)
		if err != nil {
			return "", err
		}
	}

	m := minify.New()
	m.Add("text/html", &html.Minifier{})
	r := m.Reader("text/html", &buf)
	var buf1 bytes.Buffer
	defer buf1.Reset()
	_, err := io.Copy(&buf1, r)
	if err != nil {
		return "", err
	}
	value := buf1.String()
	return value, nil
}
