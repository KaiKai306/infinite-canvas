package service

import (
	"net/http"
	"strings"
	"time"

	"github.com/tigerowo/infinite-canvas/config"
)

const FileSessionCookie = "canvas_file_session"

// Only read-file middleware accepts this cookie. Writes still require a Bearer token.
func SetFileSessionCookie(w http.ResponseWriter, token string) {
	claims, err := ParseToken(token)
	if err != nil || claims.ExpiresAt == nil {
		return
	}
	http.SetCookie(w, &http.Cookie{Name: FileSessionCookie, Value: token, Path: "/api", HttpOnly: true,
		Secure: config.Cfg.AuthCookieSecure, SameSite: http.SameSiteLaxMode,
		Expires: claims.ExpiresAt.Time, MaxAge: int(time.Until(claims.ExpiresAt.Time).Seconds())})
	w.Header().Set("Cache-Control", "no-store")
}

func ClearFileSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: FileSessionCookie, Value: "", Path: "/api", HttpOnly: true,
		Secure: config.Cfg.AuthCookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	w.Header().Set("Cache-Control", "no-store")
}

func BearerToken(r *http.Request) string {
	value := r.Header.Get("Authorization")
	if !strings.HasPrefix(value, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(value, "Bearer "))
}
