package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
	"github.com/tigerowo/infinite-canvas/service"
)

func TestServerImageArchiveSurvivesBrowserDisconnect(t *testing.T) {
	const marker = "CANVAS_ARCHIVE_FLOW_CHILD"
	if os.Getenv(marker) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestServerImageArchiveSurvivesBrowserDisconnect$", "-test.timeout=35s")
		cmd.Env = append(os.Environ(), marker+"=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("archive flow failed: %v\n%s", err, out)
		}
		return
	}
	config.Cfg = config.Config{StorageDriver: "sqlite", DatabaseDSN: ":memory:", ServerImageArchive: true, AILogDir: t.TempDir()}
	db, err := repository.DB()
	if err != nil {
		t.Fatal(err)
	}
	connection, _ := db.DB()
	connection.SetMaxOpenConns(1)
	user := model.User{ID: "archive-flow", Username: "archive-flow", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	if _, err := repository.SaveUser(user); err != nil {
		t.Fatal(err)
	}
	settings := model.Settings{Private: model.PrivateSetting{Channels: []model.ModelChannel{{ID: "fixture", Name: "fixture", Protocol: "openai", BaseURL: "https://model.invalid", APIKey: "fixture-model-key", Models: []string{"gpt-image-2"}, Enabled: true, Weight: 1}}, Storage: model.PrivateStorageSetting{Providers: []model.StorageProvider{{ID: "fixture-r2", Type: "s3", Endpoint: "https://storage.invalid", Bucket: "fixture", Region: "auto", AccessKeyID: "fixture", SecretAccessKey: "fixture", Enabled: true}}}}}
	if _, err := repository.SaveSettings(settings, time.Now().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a1H8AAAAASUVORK5CYII="
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	protocolMockHTTP(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "model.invalid" {
			return nil, errors.New("unexpected upstream host")
		}
		close(started)
		<-release
		if r.Context().Err() != nil {
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"data":[{"b64_json":"` + png + `"}]}`))}, nil
	})
	client := service.SafeProxyHTTPClient()
	previous := client.Transport
	t.Cleanup(func() { client.Transport = previous })
	client.Transport = protocolTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "storage.invalid" || r.Method != "PUT" {
			return nil, errors.New("unexpected storage request")
		}
		data, _ := io.ReadAll(r.Body)
		want, _ := base64.StdEncoding.DecodeString(png)
		if !bytes.Equal(data, want) {
			return nil, errors.New("stored bytes changed")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	browser, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/canvas/image-tasks", strings.NewReader(`{"source":"image-workbench","clientTaskId":"closed-browser","channelId":"fixture","prompt":"fixture","request":{"model":"gpt-image-2","prompt":"fixture"}}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(service.WithUser(browser, model.PublicUser(user)))
	w := httptest.NewRecorder()
	CreateCanvasImageTask(w, req)
	var result struct {
		Code int `json:"code"`
	}
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Code != 0 {
		t.Fatal("task was not accepted")
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("model request not started")
	}
	cancel()
	once.Do(func() { close(release) }) // No browser polling or upload follows.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		task, found, err := repository.GetUserCanvasImageTask(user.ID, "closed-browser")
		if err != nil || !found {
			t.Fatal("task missing")
		}
		if task.Status == "failed" {
			t.Fatal("disconnected browser cancelled server generation")
		}
		if task.Status == "completed" {
			if !strings.HasPrefix(task.StorageKey, "server:") || strings.HasPrefix(task.ImageURL, "data:") || len(task.ImageStorage) != 1 {
				t.Fatal("server did not archive without browser")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("server archival did not finish")
}
