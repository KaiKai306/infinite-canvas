package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/tigerowo/infinite-canvas/handler"
	"github.com/tigerowo/infinite-canvas/service"
	"net/http"
)

// Deliberately separate from website JWT and file-cookie authentication.
func ImageAPIAuth(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	user, credential, ok := service.AuthenticateImageAPI(service.BearerToken(c.Request))
	if !ok {
		handler.FailWithStatus(c.Writer, http.StatusUnauthorized, "生图凭证无效、已撤销或已过期")
		c.Abort()
		return
	}
	ctx := service.WithUser(c.Request.Context(), user)
	ctx = service.WithImageAPICredential(ctx, credential)
	c.Request = c.Request.WithContext(ctx)
	c.Next()
}
