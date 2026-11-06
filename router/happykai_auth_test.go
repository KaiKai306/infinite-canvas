package router_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
	"github.com/tigerowo/infinite-canvas/router"
	"github.com/tigerowo/infinite-canvas/service"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHappyKaiAccountAndPrivateMedia(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previous := config.Cfg
	t.Cleanup(func() { config.Cfg = previous })
	config.Cfg = config.Config{StorageDriver: "sqlite", DatabaseDSN: ":memory:", JWTSecret: "test-only-signing-secret", JWTExpireHours: 8, AuthCookieSecure: true}
	db, err := repository.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	defer sqlDB.Close()
	rootRole, status, wrongIdentity := 1, 1, false
	mode := "legacy"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/user/login":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["password"] != "test-password" {
				fmt.Fprint(w, `{"success":false}`)
				return
			}
			if mode == "challenge" {
				http.SetCookie(w, &http.Cookie{Name: "pending", Value: "test", Path: "/"})
				fmt.Fprint(w, `{"success":true,"data":{"require_2fa":true,"id":42}}`)
				return
			}
			if mode == "modern-challenge" {
				fmt.Fprint(w, `{"success":true,"data":{"require_verification":true,"flow_token":"test-flow"}}`)
				return
			}
			if mode == "incomplete" {
				fmt.Fprint(w, `{"success":true,"data":{}}`)
				return
			}
			if mode == "modern" {
				fmt.Fprint(w, `{"success":true,"data":{"user":{"id":42},"token_type":"Bearer","access_token":"fixture-session"}}`)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "test-session", Path: "/"})
			fmt.Fprint(w, `{"success":true,"data":{"id":42}}`)
		case "/api/user/login/2fa", "/api/user/login/verify":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			valid := body["code"] == "123456"
			if r.URL.Path == "/api/user/login/2fa" {
				_, err := r.Cookie("pending")
				valid = valid && err == nil
			} else {
				valid = valid && body["flow_token"] == "test-flow" && body["method"] == "two_fa"
			}
			if !valid {
				fmt.Fprint(w, `{"success":false}`)
				return
			}
			if mode == "modern-challenge" {
				fmt.Fprint(w, `{"success":true,"data":{"user":{"id":42},"token_type":"Bearer","access_token":"fixture-session"}}`)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "test-session", Path: "/"})
			fmt.Fprint(w, `{"success":true,"data":{"id":42}}`)
		case "/api/user/self":
			cookie, err := r.Cookie("session")
			if r.Header.Get("New-Api-User") != "42" || (r.Header.Get("Authorization") != "Bearer fixture-session" && (err != nil || cookie.Value != "test-session")) {
				fmt.Fprint(w, `{"success":false}`)
				return
			}
			id := 42
			if wrongIdentity {
				id = 43
			}
			fmt.Fprintf(w, `{"success":true,"data":{"id":%d,"username":"real-user","display_name":"Display","role":%d,"status":%d}}`, id, rootRole, status)
		default:
			t.Errorf("unexpected New API call: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	config.Cfg.NewAPIBaseURL = upstream.URL
	engine := router.New()
	request := func(method, path, body, token string, cookie *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		return w
	}
	decode := func(w *httptest.ResponseRecorder) map[string]any {
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	login := func() *httptest.ResponseRecorder {
		return request("POST", "/api/auth/login", `{"username":"real-user","password":"test-password"}`, "", nil)
	}
	if result := decode(request("POST", "/api/auth/register", `{"username":"other","password":"pass"}`, "", nil)); result["code"] == float64(0) {
		t.Fatal("local registration bypassed New API mode")
	}
	if result := decode(request("POST", "/api/auth/login", `{"username":"x","password":"wrong"}`, "", nil)); result["code"] == float64(0) {
		t.Fatal("wrong password accepted")
	}
	w := login()
	result := decode(w)
	if result["code"] != float64(0) {
		t.Fatal("legacy login failed")
	}
	data := result["data"].(map[string]any)
	token := data["token"].(string)
	if data["user"].(map[string]any)["id"] != "newapi-42" {
		t.Fatal("identity not stable")
	}
	fileCookie := w.Result().Cookies()[0]
	if !fileCookie.HttpOnly || !fileCookie.Secure || fileCookie.SameSite != http.SameSiteLaxMode || fileCookie.Path != "/api" {
		t.Fatal("unsafe file cookie")
	}
	for _, badMode := range []string{"incomplete"} {
		mode = badMode
		if decode(login())["code"] == float64(0) {
			t.Fatal("incomplete identity accepted")
		}
	}
	mode = "modern"
	if decode(login())["code"] != float64(0) {
		t.Fatal("modern login failed")
	}
	status = 2
	if decode(login())["code"] == float64(0) {
		t.Fatal("disabled upstream account accepted")
	}
	status = 1
	wrongIdentity = true
	if decode(login())["code"] == float64(0) {
		t.Fatal("mismatched self accepted")
	}
	wrongIdentity = false
	rootRole = 100
	if decode(login())["data"].(map[string]any)["user"].(map[string]any)["role"] != "admin" {
		t.Fatal("root mapping failed")
	}
	rootRole = 1
	_ = login()
	for _, challengeMode := range []string{"challenge", "modern-challenge"} {
		mode = challengeMode
		c := decode(login())["data"].(map[string]any)
		if c["requireTwoFA"] != true || c["token"] != "" || len(login().Result().Cookies()) != 0 {
			t.Fatal("challenge granted a session")
		}
		id := c["challengeId"].(string)
		body := fmt.Sprintf(`{"challengeId":%q,"verificationCode":"123456"}`, id)
		if decode(request("POST", "/api/auth/login", body, "", nil))["code"] != float64(0) {
			t.Fatal("two factor completion failed")
		}
		if decode(request("POST", "/api/auth/login", body, "", nil))["code"] == float64(0) {
			t.Fatal("challenge replay accepted")
		}
	}
	if _, err := repository.SaveUser(model.User{ID: "other-user", Username: "other-user", Role: model.UserRoleUser, Status: model.UserStatusActive}); err != nil {
		t.Fatal(err)
	}
	object := model.StorageObject{ID: "private-file", CreatedBy: "newapi-42", PublicURL: "https://storage.invalid/private.png", MimeType: "image/png", Bytes: 6}
	if _, err := repository.SaveStorageObject(object); err != nil {
		t.Fatal(err)
	}
	client := service.SafeProxyHTTPClient()
	oldTransport := client.Transport
	t.Cleanup(func() { client.Transport = oldTransport })
	reads := 0
	client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		reads++
		response := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("abcdef")), ContentLength: 6}
		if r.Header.Get("Range") == "bytes=1-3" {
			response.StatusCode = 206
			response.Header.Set("Content-Range", "bytes 1-3/6")
			response.Body = io.NopCloser(strings.NewReader("bcd"))
			response.ContentLength = 3
		}
		return response, nil
	})
	if request("GET", "/api/files/private-file/content", "", "", nil).Code != 401 {
		t.Fatal("anonymous media accepted")
	}
	if request("GET", "/api/files/private-file/content", "", "invalid", fileCookie).Code != 401 {
		t.Fatal("invalid bearer fell back to cookie")
	}
	if request("GET", "/api/files/private-file", "", "", fileCookie).Code != 200 {
		t.Fatal("owner metadata denied")
	}
	otherToken := signTestToken(t, "other-user")
	other := decode(request("GET", "/api/files/private-file/content", "", otherToken, nil))
	if other["code"] == float64(0) || reads != 0 {
		t.Fatal("cross-account media read reached storage")
	}
	owner := request("GET", "/api/files/private-file/content", "", "", fileCookie)
	if owner.Code != 200 || owner.Body.String() != "abcdef" || owner.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("owner media failed or publicly cached")
	}
	req := httptest.NewRequest("GET", "/api/files/private-file/content", nil)
	req.AddCookie(fileCookie)
	req.Header.Set("Range", "bytes=1-3")
	rangeResponse := httptest.NewRecorder()
	engine.ServeHTTP(rangeResponse, req)
	if rangeResponse.Code != 206 || rangeResponse.Body.String() != "bcd" || rangeResponse.Header().Get("Content-Range") != "bytes 1-3/6" {
		t.Fatal("range read failed")
	}
	if request("POST", "/api/v1/canvas/projects", `{}`, "", fileCookie).Code != 401 {
		t.Fatal("file cookie authenticated a write")
	}
	refreshed := request("GET", "/api/auth/me", "", token, nil)
	if len(refreshed.Result().Cookies()) == 0 {
		t.Fatal("restored login did not refresh file cookie")
	}
	cleared := request("POST", "/api/auth/logout", `{}`, "", fileCookie).Result().Cookies()[0]
	if cleared.MaxAge >= 0 {
		t.Fatal("logout failed to clear cookie")
	}
	if _, err := repository.SaveUser(model.User{ID: "newapi-42", Username: "newapi-42", Role: model.UserRoleUser, Status: model.UserStatusBan}); err != nil {
		t.Fatal(err)
	}
	if request("GET", "/api/files/private-file/content", "", "", fileCookie).Code != 401 {
		t.Fatal("locally banned user retained file access")
	}
}
