package router_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math/rand"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
	"github.com/tigerowo/infinite-canvas/router"
	"github.com/tigerowo/infinite-canvas/service"
)

func TestDedicatedImageAPIAndAccountWorks(t *testing.T) {
	const marker = "KAI_IMAGE_API_TEST_CHILD"
	if os.Getenv(marker) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDedicatedImageAPIAndAccountWorks$", "-test.timeout=55s")
		cmd.Env = append(os.Environ(), marker+"=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("image API integration failed: %v\n%s", err, out)
		}
		return
	}
	gin.SetMode(gin.TestMode)
	config.Cfg = config.Config{StorageDriver: "sqlite", DatabaseDSN: ":memory:", JWTSecret: "image-api-test-only", JWTExpireHours: 8, ServerImageArchive: true, AILogDir: t.TempDir()}
	db, err := repository.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	owner := model.User{ID: "image-api-owner", Username: "image-api-owner", Role: model.UserRoleUser, Status: model.UserStatusActive}
	other := model.User{ID: "image-api-other", Username: "image-api-other", Role: model.UserRoleUser, Status: model.UserStatusActive}
	for _, user := range []model.User{owner, other} {
		if _, err := repository.SaveUser(user); err != nil {
			t.Fatal(err)
		}
		ctx := service.WithUser(context.Background(), model.PublicUser(user))
		_, err := service.SaveCurrentUserModelConfig(ctx, json.RawMessage(`{"imageModel":"gpt-image-2","imageChannelId":"personal","localChannels":[{"id":"personal","name":"Own key","protocol":"openai","baseUrl":"https://model.invalid/v1","apiKey":"fixture-personal-key","models":["gpt-image-2"]}]}`))
		if err != nil {
			t.Fatal(err)
		}
	}
	settings := model.Settings{Private: model.PrivateSetting{Storage: model.PrivateStorageSetting{AllowUserGlobalProvider: true, Providers: []model.StorageProvider{{ID: "api-fixture-storage", Type: "s3", Endpoint: "https://storage.invalid", Bucket: "fixture", Region: "auto", AccessKeyID: "fixture-id", SecretAccessKey: "fixture-secret", Enabled: true}}}}}
	if _, err := repository.SaveSettings(settings, time.Now().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	picture := image.NewRGBA(image.Rect(0, 0, 1024, 768))
	random := rand.New(rand.NewSource(1))
	for y := 0; y < 768; y++ {
		for x := 0; x < 1024; x++ {
			picture.SetRGBA(x, y, color.RGBA{uint8(random.Intn(256)), uint8(random.Intn(256)), uint8(random.Intn(256)), 255})
		}
	}
	var original bytes.Buffer
	if err := png.Encode(&original, picture); err != nil {
		t.Fatal(err)
	}
	picture.SetRGBA(0, 0, color.RGBA{255, 255, 255, 255})
	var alternate bytes.Buffer
	if err := png.Encode(&alternate, picture); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	previous := http.DefaultTransport
	http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "model.invalid" {
			return nil, fmt.Errorf("unexpected model host")
		}
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer fixture-personal-key" {
			return nil, fmt.Errorf("did not use owner's saved key")
		}
		results := []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(original.Bytes())}}
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			var request struct {
				N int `json:"n"`
			}
			_ = json.NewDecoder(r.Body).Decode(&request)
			if request.N == 2 {
				results = append(results, map[string]string{"b64_json": base64.StdEncoding.EncodeToString(alternate.Bytes())})
			}
		}
		encoded, _ := json.Marshal(map[string]any{"data": results})
		payload := string(encoded)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(payload))}, nil
	})
	defer func() { http.DefaultTransport = previous }()
	stored := map[string][]byte{}
	var mu sync.Mutex
	client := service.SafeProxyHTTPClient()
	oldTransport := client.Transport
	defer func() { client.Transport = oldTransport }()
	client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "storage.invalid" {
			return nil, fmt.Errorf("unexpected storage host")
		}
		mu.Lock()
		defer mu.Unlock()
		status := 200
		payload := []byte{}
		switch r.Method {
		case "PUT":
			payload, _ = io.ReadAll(r.Body)
			stored[r.URL.Path] = append([]byte(nil), payload...)
			payload = nil
		case "GET":
			var found bool
			payload, found = stored[r.URL.Path]
			if !found {
				status = 404
			}
		case "DELETE":
			delete(stored, r.URL.Path)
		default:
			return nil, fmt.Errorf("unexpected storage method")
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(payload)), ContentLength: int64(len(payload))}, nil
	})
	app := router.New()
	jwtOwner, jwtOther := signTestToken(t, owner.ID), signTestToken(t, other.ID)
	request := func(method, path, token, key string, body []byte, contentType string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		w := httptest.NewRecorder()
		app.ServeHTTP(w, r)
		return w
	}
	data := func(w *httptest.ResponseRecorder) json.RawMessage {
		var value struct {
			Code int             `json:"code"`
			Data json.RawMessage `json:"data"`
		}
		if json.Unmarshal(w.Body.Bytes(), &value) != nil || value.Code != 0 {
			t.Fatalf("API envelope rejected, HTTP %d", w.Code)
		}
		return value.Data
	}
	newCredential := func(token string) (model.ImageAPICredential, string) {
		w := request("POST", "/api/v1/image-api/credentials", token, "", []byte(`{"name":"fixture","channelId":"personal","model":"gpt-image-2","days":7}`), "application/json")
		var result struct {
			Credential model.ImageAPICredential `json:"credential"`
			Secret     string                   `json:"secret"`
		}
		if json.Unmarshal(data(w), &result) != nil || result.Secret == "" {
			t.Fatal("credential not issued")
		}
		return result.Credential, result.Secret
	}
	credential, key := newCredential(jwtOwner)
	_, otherKey := newCredential(jwtOther)
	if w := request("POST", "/api/v1/image-api/credentials", "", "", []byte(`{}`), "application/json"); w.Code != 401 {
		t.Fatal("guest created key")
	}
	for _, path := range []string{"/api/v1/user-config", "/api/admin/settings", "/api/v1/image-api/credentials"} {
		if request("GET", path, key, "", nil, "").Code != 401 {
			t.Fatal("dedicated key gained website privileges")
		}
	}
	listed := request("GET", "/api/v1/image-api/credentials", jwtOwner, "", nil, "")
	if strings.Contains(listed.Body.String(), key) || strings.Contains(listed.Body.String(), "fixture-personal-key") || strings.Contains(listed.Body.String(), "tokenHash") {
		t.Fatal("credential list disclosed secrets")
	}
	input := []byte(`{"prompt":"account gallery integration","size":"1024x1024","count":2}`)
	w := request("POST", "/api/image/v1/jobs", key, "fixture-intent-0001", input, "application/json")
	if w.Code != 202 {
		t.Fatalf("expected 202, got %d", w.Code)
	}
	var job struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(data(w), &job)
	retry := request("POST", "/api/image/v1/jobs", key, "fixture-intent-0001", input, "application/json")
	if retry.Code != 200 {
		t.Fatal("same intent was not reused")
	}
	var retried struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(data(retry), &retried)
	if retried.ID != job.ID {
		t.Fatal("idempotency changed job")
	}
	if request("POST", "/api/image/v1/jobs", key, "fixture-intent-0001", []byte(`{"prompt":"different"}`), "application/json").Code != 409 {
		t.Fatal("conflicting input accepted")
	}
	if request("GET", "/api/image/v1/jobs/"+job.ID, otherKey, "", nil, "").Code != 404 {
		t.Fatal("another user read API job")
	}
	deadline := time.Now().Add(15 * time.Second)
	var task model.CanvasImageTask
	for time.Now().Before(deadline) {
		task, _, _ = repository.GetUserCanvasImageTask(owner.ID, job.ID)
		if task.Status == "completed" {
			break
		}
		if task.Status == "failed" {
			t.Fatal("mock generation failed")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if task.Status != "completed" || calls.Load() != 1 || len(task.ImageStorage) != 2 {
		t.Fatal("generation did not archive exactly once")
	}
	works, _, err := repository.ListMediaWorks(owner.ID, model.Query{})
	if err != nil || len(works) != 2 {
		t.Fatal("completed result missing from gallery without browser")
	}
	if otherWorks, _, err := repository.ListMediaWorks(other.ID, model.Query{}); err != nil || len(otherWorks) != 0 {
		t.Fatal("gallery crossed accounts")
	}
	if err := service.BuildMediaWorkThumbnail(context.Background(), works[0]); err != nil {
		t.Fatal(err)
	}
	work, err := repository.GetUserMediaWork(owner.ID, works[0].ID)
	if err != nil || work.ThumbnailID == "" {
		t.Fatal("cloud thumbnail missing")
	}
	thumb := request("GET", "/api/works/"+work.ID+"/thumbnail", jwtOwner, "", nil, "")
	if thumb.Code != 200 {
		t.Fatal("thumbnail inaccessible")
	}
	dimensions, format, err := image.DecodeConfig(bytes.NewReader(thumb.Body.Bytes()))
	if err != nil || format != "jpeg" || dimensions.Width != 512 || dimensions.Height != 384 {
		t.Fatal("wrong cloud thumbnail dimensions")
	}
	if len(thumb.Body.Bytes()) >= original.Len() {
		t.Fatal("thumbnail failed to reduce transfer size")
	}
	for _, token := range []string{"", jwtOther, otherKey, key} {
		if request("GET", "/api/works/"+work.ID+"/thumbnail", token, "", nil, "").Code == 200 {
			t.Fatal("thumbnail auth bypass")
		}
	}
	download := request("GET", "/api/image/v1/jobs/"+job.ID+"/images/0", key, "", nil, "")
	if download.Code != 200 || !bytes.Equal(download.Body.Bytes(), original.Bytes()) {
		t.Fatal("API original bytes changed")
	}
	if request("GET", "/api/image/v1/jobs/"+job.ID+"/images/0", otherKey, "", nil, "").Code != 404 {
		t.Fatal("API file crossed accounts")
	}
	objectID := strings.TrimPrefix(task.StorageKey, "server:")
	data(request("DELETE", "/api/v1/files/"+objectID, jwtOwner, "", nil, ""))
	if _, err := repository.GetStorageObject(objectID); err != nil {
		t.Fatal("history cleanup deleted gallery original")
	}
	data(request("DELETE", "/api/v1/works/"+work.ID, jwtOwner, "", nil, ""))
	if err := service.SyncMediaWorks(); err != nil {
		t.Fatal(err)
	}
	if items, _, _ := repository.ListMediaWorks(owner.ID, model.Query{}); len(items) != 1 || items[0].ID == work.ID {
		t.Fatal("removed work reappeared or another output was lost")
	}
	makeEdit := func(filename string) ([]byte, string) {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		_ = writer.WriteField("prompt", "reference edit")
		part, _ := writer.CreateFormFile("image", filename)
		_, _ = part.Write(original.Bytes())
		_ = writer.Close()
		return body.Bytes(), writer.FormDataContentType()
	}
	editBody, editType := makeEdit("reference.png")
	edit := request("POST", "/api/image/v1/jobs", key, "valid-reference-0001", editBody, editType)
	if edit.Code != 202 {
		t.Fatal("valid reference rejected")
	}
	var editJob struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(data(edit), &editJob)
	otherBody, otherType := makeEdit("renamed.png")
	if request("POST", "/api/image/v1/jobs", key, "valid-reference-0001", otherBody, otherType).Code != 200 {
		t.Fatal("reference retry depended on multipart boundary or filename")
	}
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		next, _, _ := repository.GetUserCanvasImageTask(owner.ID, editJob.ID)
		if next.Status == "completed" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	completedEdit, _, _ := repository.GetUserCanvasImageTask(owner.ID, editJob.ID)
	if calls.Load() != 2 || completedEdit.Status != "completed" {
		t.Fatal("reference edit did not complete exactly once")
	}
	// Reference upload rejects malformed images before submitting any model request.
	var multipartBody bytes.Buffer
	writer := multipart.NewWriter(&multipartBody)
	_ = writer.WriteField("prompt", "edit fixture")
	part, _ := writer.CreateFormFile("image", "fake.png")
	_, _ = part.Write([]byte("not an image"))
	_ = writer.Close()
	if request("POST", "/api/image/v1/jobs", key, "bad-reference-0001", multipartBody.Bytes(), writer.FormDataContentType()).Code != 400 || calls.Load() != 2 {
		t.Fatal("bad reference reached model")
	}
	// A reserved-but-unsubmitted intent is inspectable and is never automatically replayed.
	normalized, _ := service.NormalizeImageAPIJobInput(service.ImageAPIJobInput{Prompt: "uncertain"}, credential)
	unknown, _, err := service.ReserveImageAPIJob(owner.ID, "uncertain-intent-0001", service.ImageAPIJobFingerprint(credential, normalized, nil))
	if err != nil {
		t.Fatal(err)
	}
	response := request("POST", "/api/image/v1/jobs", key, "uncertain-intent-0001", []byte(`{"prompt":"uncertain"}`), "application/json")
	if response.Code != 200 || !strings.Contains(string(data(response)), "submission_unknown") || calls.Load() != 2 || unknown.ID == "" {
		t.Fatal("uncertain request was replayed")
	}
	if err := db.Create(&model.CanvasImageTask{ID: unknown.ID, UserID: owner.ID, Source: "api", Status: "processing", ImageURL: ""}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkInterruptedImageAPIRequests(time.Now().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	restarted := request("GET", "/api/image/v1/jobs/"+unknown.ID, key, "", nil, "")
	if !strings.Contains(string(data(restarted)), "submission_unknown") || calls.Load() != 2 {
		t.Fatal("restart did not expose unknown outcome")
	}
	for index := 0; index < 3; index++ {
		id := fmt.Sprintf("rate-fixture-%d", index)
		db.Create(&model.ImageAPIJob{ID: id, UserID: owner.ID, CreatedAt: time.Now().Format(time.RFC3339)})
		db.Create(&model.CanvasImageTask{ID: id, UserID: owner.ID, Status: "queued"})
	}
	if request("POST", "/api/image/v1/jobs", key, "rate-limit-intent-0001", input, "application/json").Code != 429 {
		t.Fatal("active API limit not enforced")
	}
	data(request("DELETE", "/api/v1/image-api/credentials/"+credential.ID, jwtOwner, "", nil, ""))
	if request("GET", "/api/image/v1/capabilities", key, "", nil, "").Code != 401 {
		t.Fatal("revoked key still usable")
	}
	expiring, expiredKey := newCredential(jwtOther)
	db.Model(&model.ImageAPICredential{}).Where("id = ?", expiring.ID).Update("expires_at", time.Now().Add(-time.Minute).Format(time.RFC3339))
	if request("GET", "/api/image/v1/capabilities", expiredKey, "", nil, "").Code != 401 {
		t.Fatal("expired key still usable")
	}
	db.Model(&model.User{}).Where("id = ?", other.ID).Update("status", model.UserStatusBan)
	if request("GET", "/api/image/v1/capabilities", otherKey, "", nil, "").Code != 401 {
		t.Fatal("banned user still usable")
	}
}
