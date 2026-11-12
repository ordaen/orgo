package handlers

import (
	"bytes"
	"fmt"
	"path"
	"reflect"
	"slices"
	"strings"
	"sync"
	"unicode"

	"github.com/gin-gonic/gin"
)

// Default is the registry of Register.
var Default = NewRegistry()

// Registry holds the registered controllers by scope, like "admins" for the admin API.
type Registry struct {
	mu     sync.RWMutex
	scopes map[string][]*Controller
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{scopes: map[string][]*Controller{}}
}

// registrable is a type embedding Controller or Resource, with its Setup method.
type registrable interface {
	Setup()
	controller() *Controller
}

// resource is implemented by the types embedding Resource.
type resource interface {
	defaultAction(name string) func(*Ctx)
	hasRepo() bool
}

// Register registers the controller in the scope of the Default registry, see Registry.Register.
func Register(scope string, c registrable) {
	Default.Register(scope, c)
}

// Register calls the Setup of the controller and registers it in the scope. It sets the ID of the controller and
// the methods, paths and default Func of its actions, see Handle.
//
// It panics when the controller has no Path, a Resource has no Repo, an action has no Func, two actions have
// the same method and path, or the scope has a controller with the same ID or Path: these are errors of the
// definitions, found at startup.
func (r *Registry) Register(scope string, c registrable) {
	c.Setup()
	ctr := c.controller()
	typeName := reflect.TypeOf(c).String()
	if ctr.Path == "" {
		panic(fmt.Sprintf("handlers: controller %s has no Path", typeName))
	}
	res, isResource := c.(resource)
	if isResource && !res.hasRepo() {
		panic(fmt.Sprintf("handlers: resource %s has no Repo", typeName))
	}
	ctr.ID = dasherize(typeName[strings.LastIndex(typeName, ".")+1:])
	ctr.scope = scope
	for i := range ctr.Actions {
		h := &ctr.Actions[i]
		setRoute(ctr.Path, h)
		if h.Func == nil && isResource {
			h.Func = res.defaultAction(h.Name)
		}
		if h.Func == nil {
			panic(fmt.Sprintf("handlers: action %q of controller %s has no Func", h.Name, typeName))
		}
		for _, prev := range ctr.Actions[:i] {
			if prev.Method == h.Method && prev.Path == h.Path {
				panic(fmt.Sprintf("handlers: actions %q and %q of controller %s are both %s %s",
					prev.Name, h.Name, typeName, h.Method, h.Path))
			}
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for _, v := range r.scopes[scope] {
		if v.ID == ctr.ID || v.Path == ctr.Path {
			panic(fmt.Sprintf("handlers: controller %s conflicts with %s (path %s) in scope %q", typeName, v.ID, v.Path, scope))
		}
	}
	r.scopes[scope] = append(r.scopes[scope], ctr)
}

// setRoute sets the method and path of the action, see Handle.
func setRoute(base string, h *Handle) {
	switch h.Name {
	case "index", "create":
		h.Path = base
		h.Method = map[string]string{"index": "GET", "create": "POST"}[h.Name]
		return
	case "show", "update", "delete":
		h.Method = map[string]string{"show": "GET", "update": "PATCH", "delete": "DELETE"}[h.Name]
		h.Path = base
		if !h.Root {
			h.Path = path.Join(base, ":id")
		}
		return
	}
	if h.Method == "" {
		h.Method = "GET"
	}
	h.Method = strings.ToUpper(h.Method)
	if h.Root {
		h.Path = path.Join(base, h.Name)
	} else {
		h.Path = path.Join(base, ":id", h.Name)
	}
}

// Scope returns the controllers of the scope in their registration order. It is encoded in JSON as the structure
// of the API: the controllers with their actions.
func (r *Registry) Scope(scope string) []*Controller {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Clone(r.scopes[scope])
}

// Routes adds the routes of the actions of the controllers of the scope to router.
//
//	handlers.Default.Routes("admins", engine.Group("/api/v1/admin"))
func (r *Registry) Routes(scope string, router gin.IRouter) {
	for _, c := range r.Scope(scope) {
		c.Routes(router)
	}
}

// Routes adds the routes of the actions to router. The handlers of a route are: the one storing the controller
// and the action in the gin context (see ActionOf), the middlewares of the controller, the middlewares of the
// action and the action.
func (c *Controller) Routes(router gin.IRouter) {
	for i := range c.Actions {
		h := &c.Actions[i]
		chain := make([]gin.HandlerFunc, 0, len(c.middles)+len(h.middles)+2)
		chain = append(chain, c.setAction(h))
		chain = append(chain, c.middles...)
		chain = append(chain, h.middles...)
		chain = append(chain, c.handler(h))
		router.Handle(h.Method, h.Path, chain...)
	}
}

const (
	controllerKey = "handlers.controller"
	actionKey     = "handlers.action"
)

// setAction returns the handler storing the controller ID and the action name in the gin context.
func (c *Controller) setAction(h *Handle) gin.HandlerFunc {
	return func(g *gin.Context) {
		g.Set(controllerKey, c.ID)
		g.Set(actionKey, h.Name)
	}
}

// ActionOf returns the ID of the controller and the name of the action of the request, like "networks" and
// "index", for the middlewares checking the permissions. They are empty outside the routes of a controller.
func ActionOf(g *gin.Context) (controller, action string) {
	return g.GetString(controllerKey), g.GetString(actionKey)
}

// handler returns the handler running the action and rendering its response.
func (c *Controller) handler(h *Handle) gin.HandlerFunc {
	return func(g *gin.Context) {
		x := &Ctx{Gin: g, controller: c}
		h.Func(x)
		x.render()
	}
}

// dasherize returns s with dashes before its capitals, lowercase: "NetworkPools" is "network-pools" and
// "APIKeys" is "api-keys".
func dasherize(s string) string {
	if s == "" {
		return ""
	}
	var (
		buf bytes.Buffer
		idx byte
		r   rune
	)
	for i, v := range s {
		if (i > 1 && idx == 0x1) || (idx == 0x2 && unicode.IsLower(v)) {
			buf.WriteByte('-')
		}
		if i > 0 {
			buf.WriteRune(r)
		}
		if unicode.IsUpper(v) {
			if idx == 0x0 {
				idx = 0x1
			} else {
				idx = 0x2
			}
			r = unicode.ToLower(v)
		} else {
			idx = 0x0
			r = v
		}
	}
	buf.WriteRune(r)
	return buf.String()
}
