package fir

import (
	"embed"
	"flag"
	"log"
	"net/http"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/gorilla/schema"
	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
	"github.com/gorilla/websocket"
	"github.com/lithammer/shortuuid/v4"
	"github.com/livefir/fir/pubsub"
)

// Controller is an interface which encapsulates a group of views. It routes requests to the appropriate view.
// It routes events to the appropriate view. It also provides a way to register views.
type Controller interface {
	Route(route Route) http.HandlerFunc
	RouteFunc(options RouteFunc) http.HandlerFunc
}

type opt struct {
	channelFunc       func(r *http.Request, viewID string) *string
	pathParamsFunc    func(r *http.Request) PathParams
	websocketUpgrader websocket.Upgrader

	disableTemplateCache bool
	disableWebsocket     bool
	debugLog             bool
	enableWatch          bool
	watchExts            []string
	publicDir            string
	developmentMode      bool
	embedFS              embed.FS
	hasEmbedFS           bool
	pubsub               pubsub.Adapter
	appName              string
	formDecoder          *schema.Decoder
	sessionStore         sessions.Store
	sessionKeyPairs      [][]byte
	sessionName          string
}

// ControllerOption is an option for the controller.
type ControllerOption func(*opt)

// WithSessionKey is an option to set the secret cookie session key for the controller.
// https://pkg.go.dev/github.com/gorilla/sessions#NewCookie
func WithSessionKeyPairs(sessionKeyPairs ...[]byte) ControllerOption {
	return func(o *opt) {
		o.sessionKeyPairs = sessionKeyPairs
	}
}

// WithSessionName is an option to set the cookie session name for the controller.
func WithSessionName(name string) ControllerOption {
	return func(o *opt) {
		o.sessionName = name
	}
}

// WithChannelFunc is an option to set a function to construct the channel name for the controller's views.
func WithChannelFunc(f func(r *http.Request, viewID string) *string) ControllerOption {
	return func(o *opt) {
		o.channelFunc = f
	}
}

func WithPathParamsFunc(f func(r *http.Request) PathParams) ControllerOption {
	return func(o *opt) {
		o.pathParamsFunc = f
	}
}

// WithPubsubAdapter is an option to set a pubsub adapter for the controller's views.
func WithPubsubAdapter(pubsub pubsub.Adapter) ControllerOption {
	return func(o *opt) {
		o.pubsub = pubsub
	}
}

// WithWebsocketUpgrader is an option to set the websocket upgrader for the controller
func WithWebsocketUpgrader(upgrader websocket.Upgrader) ControllerOption {
	return func(o *opt) {
		o.websocketUpgrader = upgrader
	}
}

// WithEmbedFS is an option to set the embed.FS for the controller.
func WithEmbedFS(fs embed.FS) ControllerOption {
	return func(o *opt) {
		o.embedFS = fs
		o.hasEmbedFS = true
	}
}

// WithPublicDir is the path to directory containing the public html template files.
func WithPublicDir(path string) ControllerOption {
	return func(o *opt) {
		o.publicDir = path
	}
}

// WithFormDecoder is an option to set the form decoder(gorilla/schema) for the controller.
func WithFormDecoder(decoder *schema.Decoder) ControllerOption {
	return func(o *opt) {
		o.formDecoder = decoder
	}
}

func WithDisableWebsocket() ControllerOption {
	return func(o *opt) {
		o.disableWebsocket = true
	}
}

// DisableTemplateCache is an option to disable template caching. This is useful for development.
func DisableTemplateCache() ControllerOption {
	return func(o *opt) {
		o.disableTemplateCache = true
	}
}

// EnableDebugLog is an option to enable debug logging.
func EnableDebugLog() ControllerOption {
	return func(o *opt) {
		o.debugLog = true
	}
}

// EnableWatch is an option to enable watching template files for changes.
func EnableWatch(rootDir string, extensions ...string) ControllerOption {
	return func(o *opt) {
		o.enableWatch = true
		if len(extensions) > 0 {
			o.publicDir = rootDir
			o.watchExts = append(o.watchExts, extensions...)
		}
	}
}

// DevelopmentMode is an option to enable development mode. It enables debug logging, template watching, and disables template caching.
func DevelopmentMode(enable bool) ControllerOption {
	return func(o *opt) {
		o.developmentMode = enable
	}
}

// NewController creates a new controller.
func NewController(name string, options ...ControllerOption) Controller {
	if name == "" {
		panic("controller name is required")
	}

	formDecoder := schema.NewDecoder()
	formDecoder.IgnoreUnknownKeys(true)
	formDecoder.SetAliasTag("json")

	validate := validator.New()
	// register function to get tag name from json tags.
	validate.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "-" {
			return ""
		}
		return name
	})

	o := &opt{
		channelFunc:       defaultChannelFunc,
		websocketUpgrader: websocket.Upgrader{EnableCompression: true},
		watchExts:         defaultWatchExtensions,
		pubsub:            pubsub.NewInmem(),
		appName:           name,
		formDecoder:       formDecoder,
		sessionKeyPairs:   [][]byte{[]byte(securecookie.GenerateRandomKey(32))},
		sessionName:       "_fir_session_",
	}

	for _, option := range options {
		option(o)
	}

	o.sessionStore = sessions.NewCookieStore(o.sessionKeyPairs...)

	if o.publicDir == "" {
		var publicDir string
		publicDirUsage := "public directory that contains the html template files."
		flag.StringVar(&publicDir, "public", ".", publicDirUsage)
		flag.StringVar(&publicDir, "p", ".", publicDirUsage+" (shortand)")
		flag.Parse()
		o.publicDir = publicDir
	}

	c := &controller{
		opt:    *o,
		name:   name,
		routes: make(map[string]*route),
	}
	if c.developmentMode {
		log.Println("controller starting in developer mode ...", c.developmentMode)
		c.debugLog = true
		c.enableWatch = true
		c.disableTemplateCache = true
	}

	if c.enableWatch {
		go watchTemplates(c)
	}

	if c.hasEmbedFS {
		log.Println("read template files embedded in the binary")
	} else {
		log.Println("read template files from disk")
	}
	return c
}

type controller struct {
	name   string
	routes map[string]*route
	opt
}

var defaultRouteOpt = &routeOpt{
	id:                shortuuid.New(),
	content:           "Hello Fir App!",
	layoutContentName: "content",
	partials:          []string{"./routes/partials"},
	funcMap:           defaultFuncMap(),
	extensions:        []string{".gohtml", ".gotmpl", ".html", ".tmpl"},
	eventSender:       make(chan Event),
	onLoad: func(ctx RouteContext) error {
		return nil
	},
}

// RouteFunc returns an http.HandlerFunc that renders the route
func (c *controller) Route(route Route) http.HandlerFunc {
	for _, option := range route.Options() {
		option(defaultRouteOpt)
	}

	r := newRoute(c, defaultRouteOpt)
	c.routes[r.id] = r
	return r.ServeHTTP
}

// RouteFunc returns an http.HandlerFunc that renders the route
func (c *controller) RouteFunc(opts RouteFunc) http.HandlerFunc {
	for _, option := range opts() {
		option(defaultRouteOpt)
	}
	r := newRoute(c, defaultRouteOpt)
	c.routes[r.id] = r
	return r.ServeHTTP
}
