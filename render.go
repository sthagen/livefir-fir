package fir

import (
	"bytes"
	"fmt"
	"html/template"
	"io"

	"github.com/livefir/fir/internal/dom"
	"github.com/livefir/fir/internal/eventstate"
	"github.com/livefir/fir/pubsub"
	"github.com/patrickmn/go-cache"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/html"
	"k8s.io/klog/v2"
)

// renderDOMEvents renders the DOM events for the given pubsub event.
// the associated templates for the event are rendered and the dom events are returned.
func renderDOMEvents(ctx RouteContext, pubsubEvent pubsub.Event) []dom.Event {
	eventIDWithState := fmt.Sprintf("%s:%s", *pubsubEvent.ID, pubsubEvent.State)
	templateNames := ctx.route.bindings.TemplateNames(eventIDWithState)
	var events []dom.Event
	for _, templateName := range templateNames {
		if templateName == "-" {
			events = append(events, dom.Event{
				ID:     *pubsubEvent.ID,
				State:  pubsubEvent.State,
				Type:   fir(eventIDWithState),
				Target: pubsubEvent.Target,
				Detail: pubsubEvent.Detail,
			})
			continue
		}
		eventType := fir(eventIDWithState, templateName)
		templateData := pubsubEvent.Detail
		if pubsubEvent.State == eventstate.Error && pubsubEvent.Detail != nil {
			errs, ok := pubsubEvent.Detail.(map[string]any)
			if !ok {
				klog.Errorf("Bindings.Events error: %s", "pubsubEvent.Detail is not a map[string]any")
				continue
			}
			templateData = map[string]any{"fir": newRouteDOMContext(ctx, errs)}
		}
		value, err := buildTemplateValue(ctx.route.template, templateName, templateData)
		if err != nil {
			klog.Errorf("Bindings.Events buildTemplateValue error for eventType: %v, err: %v", *eventType, err)
			continue
		}
		if pubsubEvent.State == eventstate.Error && value == "" {
			continue
		}

		events = append(events, dom.Event{
			ID:     eventIDWithState,
			State:  pubsubEvent.State,
			Type:   eventType,
			Target: pubsubEvent.Target,
			Detail: value,
		})
	}

	return trackErrors(ctx, pubsubEvent, events)
}

func trackErrors(ctx RouteContext, pubsubEvent pubsub.Event, events []dom.Event) []dom.Event {
	var prevErrors map[string]string
	v, ok := ctx.route.cache.Get(*pubsubEvent.SessionID)
	if ok {
		prevErrors, ok = v.(map[string]string)
		if !ok {
			panic("fir: cache value is not a map[string]string")
		}
	} else {
		prevErrors = make(map[string]string)
	}

	newErrors := make(map[string]string)
	var newEvents []dom.Event
	// set new errors & add events to newEvents
	for _, event := range events {
		if event.State == eventstate.OK {
			newEvents = append(newEvents, event)
			continue
		}
		newErrors[*event.Type] = *event.Target
		newEvents = append(newEvents, event)
	}
	// set new errors
	ctx.route.cache.Set(*pubsubEvent.SessionID, newErrors, cache.DefaultExpiration)
	// unset previously set errors
	for k, v := range prevErrors {
		k := k
		v := v
		eventType := &k
		target := v
		if _, ok := newErrors[*eventType]; ok {
			continue
		}
		newEvents = append(newEvents, dom.Event{
			Type:   eventType,
			Target: &target,
			Detail: "",
		})
	}

	if len(newEvents) == 0 {
		eventIDWithState := fmt.Sprintf("%s:%s", *pubsubEvent.ID, pubsubEvent.State)
		newEvents = append(newEvents, dom.Event{
			ID:     *pubsubEvent.ID,
			State:  pubsubEvent.State,
			Type:   fir(eventIDWithState),
			Target: pubsubEvent.Target,
			Detail: pubsubEvent.Detail,
		})
	}
	return newEvents
}

func buildTemplateValue(t *template.Template, name string, data any) (string, error) {
	if t == nil {
		return "", nil
	}
	if name == "" {
		return "", nil
	}
	var dataBuf bytes.Buffer
	defer dataBuf.Reset()
	if name == "_fir_html" {
		dataBuf.WriteString(data.(string))
	} else {
		t.Option("missingkey=zero")
		err := t.ExecuteTemplate(&dataBuf, name, data)
		if err != nil {
			return "", err
		}
	}

	m := minify.New()
	m.Add("text/html", &html.Minifier{})
	rd := m.Reader("text/html", &dataBuf)
	var minBuf bytes.Buffer
	defer minBuf.Reset()
	_, err := io.Copy(&minBuf, rd)
	if err != nil {
		return "", err
	}
	value := minBuf.String()
	return value, nil
}
