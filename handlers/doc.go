// Package handlers serves REST controllers of models with gin.
//
// A controller embeds Controller, or Resource for the records of a repository with the default actions index,
// show, create, update and delete. Its Setup method sets its path and actions, and it is registered in a scope
// with Register. The routes of a scope are added to a gin router with Registry.Routes:
//
//	type Networks struct{ handlers.Resource[*models.Network] }
//
//	func (c *Networks) Setup() {
//		c.Name = "Networks"
//		c.Path = "/networks"
//		c.Repo = models.Networks
//		c.Middlewares(CheckLogin)
//		c.Action("index", handlers.Handle{Desc: "fetch networks"})
//		c.Action("show", handlers.Handle{Desc: "fetch network by id"})
//		c.Action("whois", handlers.Handle{Desc: "whois record", Func: c.whois})
//	}
//
//	func init() { handlers.Register("admins", &Networks{}) }
//
//	handlers.Default.Routes("admins", engine.Group("/api/v1/admin"))
//
// An action gets the request as a Ctx and sets the response: {"data": ..., "meta": ..., "errors": [...]}.
// The list actions read the URL query described by Query, see GetRecords. The records are written with their
// changes logged by the user of the request, see CreateRecord and UpdateRecord.
package handlers
