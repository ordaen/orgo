package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/ordaen/orgo/logger"
	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/repo"
	"github.com/ordaen/orgo/sessions"
	"github.com/ordaen/orgo/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var itemsRepo = repo.New(&item{})

// Items is a resource with the default actions, a custom index and custom actions.
type Items struct{ Resource[*item] }

// seen are the controller and action of the last request seen by the middleware of Items.
var seen [2]string

func (c *Items) Setup() {
	c.Name = "Items"
	c.Group = "items"
	c.Path = "/items"
	c.Repo = itemsRepo
	c.AddTags("items")
	c.Middlewares(func(g *gin.Context) {
		seen[0], seen[1] = ActionOf(g)
		if g.GetHeader("X-Deny") != "" {
			RenderError(g, ErrorUnauthorized)
		}
	}, withSession)
	c.Action("index", Handle{Desc: "fetch items", Func: c.index})
	c.Action("show", Handle{Desc: "fetch item"})
	c.Action("create", Handle{Desc: "create item"})
	c.Action("update", Handle{Desc: "update item"})
	c.Action("delete", Handle{Desc: "delete item"})
	c.Action("activate", Handle{Method: "post", Desc: "activate item", Func: c.activate})
	c.Action("names", Handle{Root: true, Desc: "item names", Func: c.names}, func(g *gin.Context) {
		g.Header("X-Action-Middleware", "1")
	})
	c.Action("download", Handle{Desc: "download item", Func: c.download})
}

// withSession stores the session of the X-User header, like an authentication middleware.
func withSession(g *gin.Context) {
	if name := g.GetHeader("X-User"); name != "" {
		s := &sessions.Session{}
		s.SetUserFields(&types.User{ID: 42, Name: name, Type: "admins", IP: "10.9.9.9"})
		g.Set("session", s)
	}
}

func (c *Items) index(x *Ctx) {
	if x.QueryParam("owned") != "" {
		x.AddWhereScope("owner_id = ?", x.QueryParam("owned"))
	}
	c.Index(x)
}

func (c *Items) activate(x *Ctx) {
	rec, ok := FetchRecord(x, c.Repo, x.Param("id"))
	if !ok {
		return
	}
	rec.Active = true
	x.SetDataOrError(c.Repo.Update(rec, "active"))
}

func (c *Items) names(x *Ctx) {
	var names []string
	for _, v := range c.Repo.FindMany("") {
		names = append(names, v.Name)
	}
	x.SetData(names)
}

func (c *Items) download(x *Ctx) {
	if rec, ok := FetchRecord(x, c.Repo, x.Param("id")); ok {
		x.Gin.Data(200, "text/plain", []byte(rec.Name))
	}
}

// newTestRouter returns a router with the Items controller in a new registry.
func newTestRouter(t *testing.T) (*gin.Engine, *Registry) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	reg := NewRegistry()
	reg.Register("admins", &Items{})
	engine := gin.New()
	reg.Routes("admins", engine.Group("/api"))
	return engine, reg
}

type testResponse struct {
	code   int
	header http.Header
	body   string
}

func serve(t *testing.T, engine *gin.Engine, method, url, body string, headers ...string) testResponse {
	t.Helper()
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return testResponse{code: w.Code, header: w.Header(), body: w.Body.String()}
}

func TestResourceIndex(t *testing.T) {
	seedQueryItems(t)
	engine, _ := newTestRouter(t)

	r := serve(t, engine, "GET", "/api/items?sort=-name&fields=name&limit=2&offset=1", "")
	assert.Equal(t, 200, r.code)
	assert.Equal(t, "application/json; charset=utf-8", r.header.Get("Content-Type"))
	assert.JSONEq(t, `{"data":[{"id":"4","name":"delta_x"},{"id":"2","name":"beta"}],
		"meta":{"total":5,"limit":2,"offset":1}}`, r.body)
	assert.Equal(t, [2]string{"items", "index"}, seen)

	r = serve(t, engine, "GET", "/api/items?owned=1&fields=name", "")
	assert.JSONEq(t, `{"data":[{"id":"1","name":"alpha"},{"id":"3","name":"gamma"}],
		"meta":{"total":2,"limit":30,"offset":0}}`, r.body, "the scope of the custom index")

	r = serve(t, engine, "GET", "/api/items?filter[nope]=1", "")
	assert.Equal(t, 400, r.code)
	assert.JSONEq(t, `{"errors":[{"status":"400","title":"Bad Request","detail":"invalid filter column \"nope\""}]}`, r.body)

	r = serve(t, engine, "GET", "/api/items?filter[owner_id]=abc", "")
	assert.Equal(t, 400, r.code, "a value of the wrong type")
	assert.Contains(t, r.body, `"detail":"invalid query value: `)
}

func TestResourceMaxRecords(t *testing.T) {
	seedQueryItems(t)
	gin.SetMode(gin.TestMode)
	reg := NewRegistry()
	items := &Items{}
	reg.Register("admins", items)
	items.MaxRecords = 2
	engine := gin.New()
	reg.Routes("admins", engine)
	r := serve(t, engine, "GET", "/items?limit=100&fields=id", "")
	assert.JSONEq(t, `{"data":[{"id":"1"},{"id":"5"}],"meta":{"total":5,"limit":2,"offset":0}}`, r.body)
}

func TestResourceShow(t *testing.T) {
	seedQueryItems(t)
	engine, _ := newTestRouter(t)

	r := serve(t, engine, "GET", "/api/items/3?fields=name,ip", "")
	assert.Equal(t, 200, r.code)
	assert.JSONEq(t, `{"data":{"id":"3","name":"gamma","ip":"10.1.2.0/24"}}`, r.body)

	notFound := `{"errors":[{"status":"404","title":"Record Not Found","detail":"The record you are looking for does not exist"}]}`
	for _, url := range []string{"/api/items/99", "/api/items/abc", "/api/items/99999999999999999999"} {
		r = serve(t, engine, "GET", url, "")
		assert.Equal(t, 404, r.code, url)
		assert.JSONEq(t, notFound, r.body, url)
	}
}

// lastLog returns the last audit log.
func lastLog(t *testing.T) *logger.Log {
	t.Helper()
	logs, err := pg.Query(&logger.Log{}).Order("id DESC").Limit(1).Select()
	require.NoError(t, err)
	require.Len(t, logs, 1)
	return logs[0]
}

func TestResourceCreate(t *testing.T) {
	seedQueryItems(t)
	_, err := pg.DB.Exec(t.Context(), `SELECT setval('items_id_seq', 100)`)
	require.NoError(t, err)
	engine, _ := newTestRouter(t)

	r := serve(t, engine, "POST", "/api/items?fields=name,active",
		`{"id":"7","name":"new","active":true,"owner_id":"2"}`, "X-User", "admin")
	assert.Equal(t, 200, r.code, r.body)
	assert.JSONEq(t, `{"data":{"id":"101","name":"new","active":true}}`, r.body)
	rec := itemsRepo.FindByID(101)
	assert.Equal(t, model.ID(2), rec.OwnerID)

	l := lastLog(t)
	assert.Equal(t, "create", l.Action)
	assert.Equal(t, "admin", l.UserName)
	assert.Equal(t, "192.0.2.1", l.UserIP, "the IP of the request")

	r = serve(t, engine, "POST", "/api/items", `[]`)
	assert.Equal(t, 400, r.code)
	r = serve(t, engine, "POST", "/api/items", `{"owner_id":true}`)
	assert.Equal(t, 422, r.code)
	assert.JSONEq(t, `{"errors":[{"status":"422","attribute":"owner_id","title":"Invalid Attribute",
		"detail":"owner_id is invalid: invalid ID format"}]}`, r.body)
	r = serve(t, engine, "POST", "/api/items", `{"ip":"not an ip"}`)
	assert.Equal(t, 422, r.code, "a data exception of the database")
	assert.Contains(t, r.body, `"title":"Invalid Attribute"`)
}

func TestResourceUpdate(t *testing.T) {
	seedQueryItems(t)
	engine, _ := newTestRouter(t)

	r := serve(t, engine, "PATCH", "/api/items/2?fields=name,owner_id", `{"name":"beta2","owner_id":"9"}`, "X-User", "editor")
	assert.Equal(t, 200, r.code, r.body)
	assert.JSONEq(t, `{"data":{"id":"2","name":"beta2","owner_id":"9"}}`, r.body)
	rec := itemsRepo.FindByID(2)
	assert.Equal(t, "beta2", rec.Name)
	assert.Equal(t, model.ID(9), rec.OwnerID)

	l := lastLog(t)
	assert.Equal(t, "update", l.Action)
	assert.Equal(t, "editor", l.UserName)
	assert.ElementsMatch(t, []string{"name", "owner_id"}, l.Changes.Keys())

	logsBefore := logger.Logs.Count()
	r = serve(t, engine, "PATCH", "/api/items/2", `{"name":"beta2"}`)
	assert.Equal(t, 200, r.code)
	assert.Equal(t, logsBefore, logger.Logs.Count(), "nothing is written without changes")

	r = serve(t, engine, "PATCH", "/api/items/99", `{"name":"x"}`)
	assert.Equal(t, 404, r.code)
}

func TestResourceDelete(t *testing.T) {
	seedQueryItems(t)
	engine, _ := newTestRouter(t)

	r := serve(t, engine, "DELETE", "/api/items/4", "")
	assert.Equal(t, 204, r.code)
	assert.Empty(t, r.body)
	assert.False(t, itemsRepo.FindByID(4).ID.Valid())
	assert.Equal(t, "delete", lastLog(t).Action)
	assert.Equal(t, "Undefined", lastLog(t).UserName, "without a session")

	r = serve(t, engine, "DELETE", "/api/items/4", "")
	assert.Equal(t, 404, r.code)
}

func TestCustomActions(t *testing.T) {
	seedQueryItems(t)
	engine, _ := newTestRouter(t)

	r := serve(t, engine, "POST", "/api/items/2/activate", "")
	assert.Equal(t, 200, r.code)
	assert.True(t, itemsRepo.FindByID(2).Active)
	assert.Equal(t, [2]string{"items", "activate"}, seen)

	r = serve(t, engine, "GET", "/api/items/names", "")
	assert.Equal(t, 200, r.code, "a root action next to the :id routes")
	assert.Equal(t, "1", r.header.Get("X-Action-Middleware"))
	var names struct{ Data []string }
	require.NoError(t, json.Unmarshal([]byte(r.body), &names))
	assert.Len(t, names.Data, 5)

	r = serve(t, engine, "GET", "/api/items/3/download", "")
	assert.Equal(t, 200, r.code)
	assert.Equal(t, "text/plain", r.header.Get("Content-Type"))
	assert.Equal(t, "gamma", r.body, "the response written by the action")

	r = serve(t, engine, "GET", "/api/items/3", "", "X-Deny", "1")
	assert.Equal(t, 401, r.code)
	assert.JSONEq(t, `{"errors":[{"status":"401","title":"Unauthorized Request","detail":"You are forbidden from accessing this page"}]}`, r.body)
}

func TestRegistryStructure(t *testing.T) {
	_, reg := newTestRouter(t)
	ctrs := reg.Scope("admins")
	require.Len(t, ctrs, 1)
	assert.Empty(t, reg.Scope("other"))
	assert.Equal(t, "admins", ctrs[0].Scope())

	b, err := json.Marshal(ctrs)
	require.NoError(t, err)
	assert.JSONEq(t, `[{"id":"items","path":"/items","name":"Items","group":"items","tags":["items"],"actions":[
		{"name":"index","path":"/items","method":"GET","desc":"fetch items"},
		{"name":"show","path":"/items/:id","method":"GET","desc":"fetch item"},
		{"name":"create","path":"/items","method":"POST","desc":"create item"},
		{"name":"update","path":"/items/:id","method":"PATCH","desc":"update item"},
		{"name":"delete","path":"/items/:id","method":"DELETE","desc":"delete item"},
		{"name":"activate","path":"/items/:id/activate","method":"POST","desc":"activate item"},
		{"name":"names","path":"/items/names","method":"GET","desc":"item names"},
		{"name":"download","path":"/items/:id/download","method":"GET","desc":"download item"}]}]`, string(b))
}

type plainCtr struct {
	Controller
	setup func(c *Controller)
}

func (c *plainCtr) Setup() { c.setup(&c.Controller) }

type NetworkPools struct{ plainCtr }

type noRepo struct{ Resource[*item] }

func (c *noRepo) Setup() { c.Path = "/x" }

func TestRegisterPanics(t *testing.T) {
	ok := func(c *Controller) { c.Path = "/ok"; c.Action("x", Handle{Func: func(*Ctx) {}}) }
	for name, register := range map[string]func(r *Registry){
		"no path":   func(r *Registry) { r.Register("s", &plainCtr{setup: func(*Controller) {}}) },
		"no repo":   func(r *Registry) { r.Register("s", &noRepo{}) },
		"no func":   func(r *Registry) { r.Register("s", &plainCtr{setup: func(c *Controller) { c.Path = "/a"; c.Action("index", Handle{}) }}) },
		"same path": func(r *Registry) {
			r.Register("s", &plainCtr{setup: func(c *Controller) {
				c.Path = "/a"
				c.Action("x", Handle{Root: true, Func: func(*Ctx) {}})
				c.Action("x", Handle{Root: true, Method: "GET", Func: func(*Ctx) {}})
			}})
		},
		"same id": func(r *Registry) {
			r.Register("s", &plainCtr{setup: ok})
			r.Register("s", &plainCtr{setup: func(c *Controller) { ok(c); c.Path = "/other" }})
		},
	} {
		assert.Panics(t, func() { register(NewRegistry()) }, name)
	}
	reg := NewRegistry()
	reg.Register("s", &plainCtr{setup: ok})
	reg.Register("t", &plainCtr{setup: ok})
	reg.Register("s", &NetworkPools{plainCtr{setup: func(c *Controller) { ok(c); c.Path = "/pools" }}})
	assert.Equal(t, "network-pools", reg.Scope("s")[1].ID)
}

func TestDasherize(t *testing.T) {
	for in, want := range map[string]string{
		"":                      "",
		"Session":               "session",
		"NetworkPools":          "network-pools",
		"CustomerWhoisProfiles": "customer-whois-profiles",
		"APIKeys":               "api-keys",
		"SystemInfo":            "system-info",
	} {
		assert.Equal(t, want, dasherize(in), in)
	}
}

func TestRenderErrorOfPlainError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/", func(g *gin.Context) { RenderError(g, nil) })
	r := serve(t, engine, "GET", "/", "")
	assert.Equal(t, 500, r.code)
	assert.JSONEq(t, `{"errors":[{"status":"500","title":"Internal Server Error","detail":"unknown error"}]}`, r.body)
}
