package router

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"pentagi/pkg/growth/connectors"

	"github.com/gin-gonic/gin"
)

// registerBrowserView passes the browser service's login window on under
// connectors.BrowserViewPath. Only the routes a login ticket opens go through, and
// without the operator's cookies or authorization, so the ticket alone decides access.
func registerBrowserView(router *gin.Engine, serviceURL string) error {
	target, err := url.Parse(serviceURL)
	if err != nil || target.Scheme == "" || target.Host == "" {
		return fmt.Errorf("browser service URL %q is not an absolute URL", serviceURL)
	}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.Out.URL.Path = strings.TrimPrefix(r.In.URL.Path, connectors.BrowserViewPath)
			r.Out.URL.RawPath = ""
			r.SetURL(target)
			r.Out.Header.Del("Authorization")
			r.Out.Header.Del("Cookie")
		},
	}
	router.Any(connectors.BrowserViewPath+"/*path", func(c *gin.Context) {
		path := c.Param("path")
		if !strings.HasPrefix(path, "/login/") && !strings.HasPrefix(path, "/sessions/") {
			c.Status(http.StatusNotFound)
			return
		}
		proxy.ServeHTTP(c.Writer, c.Request)
	})
	return nil
}
