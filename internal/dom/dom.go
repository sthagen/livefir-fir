package dom

import (
	"fmt"
	"html/template"
	"io"
	"regexp"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"
	"github.com/livefir/fir/internal/eventstate"
	"golang.org/x/exp/slices"
	"k8s.io/klog/v2"
)

type Event struct {
	Type   *string `json:"type,omitempty"`
	Target *string `json:"target,omitempty"`
	Detail any     `json:"detail,omitempty"`
	// Private fields
	ID    string          `json:"-"`
	State eventstate.Type `json:"-"`
}

func RouteBindings(id string, tmpl *template.Template) *Bindings {
	return &Bindings{
		id:                id,
		tmpl:              tmpl,
		eventTemplates:    make(map[string]map[string]struct{}),
		RWMutex:           &sync.RWMutex{},
		templateNameRegex: regexp.MustCompile(`^[ A-Za-z0-9\-:]*$`),
	}
}

type Bindings struct {
	id                string
	tmpl              *template.Template
	eventTemplates    map[string]map[string]struct{}
	templateNameRegex *regexp.Regexp
	*sync.RWMutex
}

func eventFormatError(eventns string) string {
	return fmt.Sprintf(`
	error: invalid event namespace: %s. must be of either of the two formats =>
	1. @fir:<event>:<ok|error>::<block-name|optional>
	2. @fir:<event>:<pending|done>`, eventns)
}

func (b *Bindings) AddFile(rd io.Reader) {
	b.Lock()
	defer b.Unlock()

	doc, err := goquery.NewDocumentFromReader(rd)
	if err != nil {
		panic(err)
	}
	doc.Find("*").Each(func(_ int, node *goquery.Selection) {
		for _, a := range node.Get(0).Attr {

			if strings.HasPrefix(a.Key, "@fir:") || strings.HasPrefix(a.Key, "x-on:fir:") {

				eventns := strings.TrimPrefix(a.Key, "@fir:")
				eventns = strings.TrimPrefix(eventns, "x-on:fir:")
				eventnsParts := strings.SplitN(eventns, ".", -1)
				if len(eventnsParts) > 3 {
					klog.Errorf(eventFormatError(eventns))
					continue
				}

				if len(eventnsParts) > 0 {
					eventns = eventnsParts[0]
				}

				// myevent:ok::myblock
				eventnsParts = strings.SplitN(eventns, "::", -1)
				if len(eventnsParts) == 0 {
					continue
				}
				// [myevent:ok, myblock]
				if len(eventnsParts) > 2 {
					klog.Errorf(eventFormatError(eventns))
					continue
				}

				// myevent:ok
				eventID := eventnsParts[0]
				// [myevent, ok]
				eventIDParts := strings.SplitN(eventID, ":", -1)
				if len(eventIDParts) != 2 {
					klog.Errorf(eventFormatError(eventns))
					continue
				}
				// event name can only be followed by ok, error, pending, done
				if !slices.Contains([]string{"ok", "error", "pending", "done"}, eventIDParts[1]) {
					klog.Errorf(eventFormatError(eventns))
					continue
				}
				// assert myevent:ok::myblock or myevent:error::myblock
				if len(eventnsParts) == 2 && !slices.Contains([]string{"ok", "error"}, eventIDParts[1]) {
					klog.Errorf(eventFormatError(eventns))
					continue

				}
				// template name is declared for event state i.e. myevent:ok::myblock
				templateName := "-"
				if len(eventnsParts) == 2 {
					templateName = eventnsParts[2]
				}

				templates, ok := b.eventTemplates[eventID]
				if !ok {
					templates = make(map[string]struct{})
				}

				if !b.templateNameRegex.MatchString(templateName) {
					klog.Errorf("error: invalid template name in event binding: only hyphen(-) and colon(:) are allowed: %v\n", templateName)
					continue
				}

				templates[templateName] = struct{}{}

				//fmt.Printf("eventID: %s, blocks: %v\n", eventID, blocks)
				b.eventTemplates[eventID] = templates
			}
		}

	})

}

func (b *Bindings) TemplateNames(eventIDWithState string) []string {
	b.RLock()
	defer b.RUnlock()
	var templateNames []string
	for k := range b.eventTemplates[eventIDWithState] {
		templateNames = append(templateNames, k)
	}
	return templateNames
}
