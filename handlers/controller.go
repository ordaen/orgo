package handlers

import (
	"github.com/gin-gonic/gin"
	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/repo"
)

// Handle is an action of a controller. The actions index, show, create, update and delete have their method and
// path: GET Path, GET Path/:id, POST Path, PATCH Path/:id and DELETE Path/:id, without :id when Root is set.
// The other actions are GET by default, with the path Path/:id/name, or Path/name when Root is set.
// Func runs the action, a Resource has the default Func of the five actions.
//
// The fields without JSON are for the API documentation.
type Handle struct {
	Name          string     `json:"name,omitempty"`
	Path          string     `json:"path,omitempty"`
	Method        string     `json:"method,omitempty"`
	Tags          []string   `json:"tags,omitempty"`
	Summary       string     `json:"summary,omitempty"`
	Desc          string     `json:"desc,omitempty"`
	OperationID   string     `json:"operationID,omitempty"`
	Consumes      []string   `json:"consumes,omitempty"`
	Produces      []string   `json:"produces,omitempty"`
	NoToken       bool       `json:"-"`
	OptionalToken bool       `json:"-"`
	Root          bool       `json:"-"`
	NoDocs        bool       `json:"-"`
	Input         any        `json:"-"`
	Output        any        `json:"-"`
	Func          func(*Ctx) `json:"-"`

	middles []gin.HandlerFunc
}

// Middlewares adds middlewares run before the action, after the middlewares of the controller.
func (h *Handle) Middlewares(mids ...gin.HandlerFunc) {
	h.middles = append(h.middles, mids...)
}

// Controller is a group of actions under a path. It is embedded in a type with a Setup method setting its
// fields and actions, registered with Register:
//
//	type Session struct{ handlers.Controller }
//
//	func (c *Session) Setup() {
//		c.Name = "Session"
//		c.Path = "/session"
//		c.Action("login", handlers.Handle{Root: true, Method: "POST", Func: c.login}, CheckIP)
//	}
//
// ID is set by Register: the dasherized name of the type, like "network-pools" for NetworkPools.
type Controller struct {
	ID      string   `json:"id,omitempty"`
	Path    string   `json:"path,omitempty"`
	Name    string   `json:"name,omitempty"`
	Group   string   `json:"group,omitempty"`
	Desc    string   `json:"desc,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	Actions []Handle `json:"actions,omitempty"`
	// MaxRecords is the maximum limit of the list queries, unlimited when it is 0.
	MaxRecords int `json:"-"`

	middles []gin.HandlerFunc
	scope   string
}

func (c *Controller) controller() *Controller { return c }

// Scope returns the scope the controller is registered in.
func (c *Controller) Scope() string {
	return c.scope
}

// AddTags adds tags to the controller, for the API documentation.
func (c *Controller) AddTags(tags ...string) {
	c.Tags = append(c.Tags, tags...)
}

// Middlewares adds middlewares run before every action of the controller.
func (c *Controller) Middlewares(mids ...gin.HandlerFunc) {
	c.middles = append(c.middles, mids...)
}

// Action adds the action name, with middlewares run before it after the middlewares of the controller.
func (c *Controller) Action(name string, h Handle, mids ...gin.HandlerFunc) {
	h.Name = name
	h.middles = append(h.middles, mids...)
	c.Actions = append(c.Actions, h)
}

// Resource is a controller of the records of Repo, with the default actions index, show, create, update and
// delete. Repo is set by Setup:
//
//	type Networks struct{ handlers.Resource[*models.Network] }
//
//	func (c *Networks) Setup() {
//		c.Name = "Networks"
//		c.Path = "/networks"
//		c.Repo = models.Networks
//		c.Action("index", handlers.Handle{Desc: "fetch networks", Func: c.index})
//		c.Action("show", handlers.Handle{Desc: "fetch network by id"})
//	}
//
//	func (c *Networks) index(x *handlers.Ctx) {
//		if v := x.QueryParam("mask"); v != "" {
//			x.AddWhereScope("masklen(ip) = ?", v)
//		}
//		c.Index(x)
//	}
type Resource[T model.Model] struct {
	Controller
	Repo repo.Repository[T]
}

// Index sets the response to a page of the records, see GetRecords.
func (r *Resource[T]) Index(c *Ctx) {
	GetRecords(c, r.Repo)
}

// Show sets the response to the record with the id parameter, with the fields of the request.
func (r *Resource[T]) Show(c *Ctx) {
	if rec, ok := FetchRecord(c, r.Repo, c.Param("id")); ok {
		c.SetFields(splitList(c.QueryParam("fields")))
		c.SetData(rec)
	}
}

// Create creates a record from the request body and sets the response to it, see CreateRecord.
func (r *Resource[T]) Create(c *Ctx) {
	if rec, ok := CreateRecord(c, r.Repo); ok {
		c.SetFields(splitList(c.QueryParam("fields")))
		c.SetData(rec)
	}
}

// Update updates the record with the id parameter from the request body and sets the response to it,
// see UpdateRecord.
func (r *Resource[T]) Update(c *Ctx) {
	rec, ok := FetchRecord(c, r.Repo, c.Param("id"))
	if !ok {
		return
	}
	if rec, ok = UpdateRecord(c, r.Repo, rec); ok {
		c.SetFields(splitList(c.QueryParam("fields")))
		c.SetData(rec)
	}
}

// Delete deletes the record with the id parameter, the response is 204 No Content.
func (r *Resource[T]) Delete(c *Ctx) {
	rec, ok := FetchRecord(c, r.Repo, c.Param("id"))
	if ok && DeleteRecord(c, r.Repo, rec) {
		c.ResponseCode = 204
	}
}

// defaultAction returns the default Func of the action, nil when it has none.
func (r *Resource[T]) defaultAction(name string) func(*Ctx) {
	switch name {
	case "index":
		return r.Index
	case "show":
		return r.Show
	case "create":
		return r.Create
	case "update":
		return r.Update
	case "delete":
		return r.Delete
	}
	return nil
}

// hasRepo reports whether Repo is set.
func (r *Resource[T]) hasRepo() bool {
	return r.Repo != nil
}
