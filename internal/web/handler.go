package web

// BaseHandler is embedded by all HTTP handlers which render templates.
type BaseHandler struct {
	ViewRenderer *Renderer
}

func NewBaseHandler(viewRenderer *Renderer) *BaseHandler {
	return &BaseHandler{
		ViewRenderer: viewRenderer,
	}
}
