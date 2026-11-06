package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/gif"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
)

type archiveTransport func(*http.Request) (*http.Response, error)

func (f archiveTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestImageArchiveDurabilityAndIsolation(t *testing.T) {
	previous := config.Cfg
	config.Cfg = config.Config{StorageDriver: "sqlite", DatabaseDSN: ":memory:"}
	t.Cleanup(func() { config.Cfg = previous })
	db, err := repository.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	owner := model.User{ID: "archive-owner", Username: "archive-owner", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if _, err := repository.SaveUser(owner); err != nil {
		t.Fatal(err)
	}
	settings := model.Settings{Private: model.PrivateSetting{Storage: model.PrivateStorageSetting{AllowUserGlobalProvider: true, Providers: []model.StorageProvider{{ID: "fixture-r2", Type: "s3", Endpoint: "https://storage.invalid", Bucket: "fixture", Region: "auto", AccessKeyID: "fixture-id", SecretAccessKey: "fixture-secret", Enabled: true}}}}}
	if _, err := repository.SaveSettings(settings, now()); err != nil {
		t.Fatal(err)
	}
	pngData, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a1H8AAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngData)
	var mu sync.Mutex
	puts, gets, deletes := 0, 0, 0
	failAt := 0
	stored := map[string][]byte{}
	client := SafeProxyHTTPClient()
	old := client.Transport
	t.Cleanup(func() { client.Transport = old })
	client.Transport = archiveTransport(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		status := 200
		body := []byte{}
		if r.URL.Host == "result.invalid" {
			gets++
			body = pngData
		} else if r.URL.Host == "storage.invalid" {
			switch r.Method {
			case "PUT":
				puts++
				if puts == failAt {
					status = 503
				} else {
					data, _ := io.ReadAll(r.Body)
					stored[r.URL.Path] = data
				}
			case "DELETE":
				deletes++
				delete(stored, r.URL.Path)
			default:
				t.Errorf("unexpected storage method %s", r.Method)
			}
		} else {
			t.Errorf("unexpected network host")
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body))}, nil
	})
	newTask := func(id string, urls ...string) model.CanvasImageTask {
		task := model.CanvasImageTask{ID: id, UserID: owner.ID, Source: "image-workbench", Status: "processing", Progress: 95, ImageURL: urls[0], CreatedAt: now(), UpdatedAt: now()}
		if len(urls) > 1 {
			task.ImageURLs = urls
		}
		if _, err := repository.SaveCanvasImageTask(task); err != nil {
			t.Fatal(err)
		}
		return task
	}
	read := func(id string) model.CanvasImageTask {
		task, found, err := repository.GetUserCanvasImageTask(owner.ID, id)
		if err != nil || !found {
			t.Fatal("missing fixture task")
		}
		return task
	}

	t.Run("storage outage preserves expiring result then retries without another model call", func(t *testing.T) {
		task := newTask("archive-retry", "https://result.invalid/temporary.png")
		if response := CanvasImageTaskResponse(task); response["image_url"] != nil {
			t.Fatal("pending raw image exposed as completed")
		}
		failAt = puts + 1
		if err := ArchiveCanvasImageTask(context.Background(), owner.ID, task.ID); err == nil {
			t.Fatal("storage outage was hidden")
		}
		pending := read(task.ID)
		if pending.Status != "processing" || !strings.HasPrefix(pending.ImageURL, "data:") || pending.StorageKey != "" {
			t.Fatal("paid result was lost or prematurely completed")
		}
		failAt = 0
		if err := ArchiveCanvasImageTask(context.Background(), owner.ID, task.ID); err != nil {
			t.Fatal(err)
		}
		done := read(task.ID)
		if done.Status != "completed" || !strings.HasPrefix(done.StorageKey, "server:") || strings.Contains(done.ImageURL, "base64") || len(done.ImageStorage) != 1 || done.Width != 1 || done.Height != 1 {
			t.Fatal("archive did not replace pixels with references")
		}
		if gets != 1 {
			t.Fatal("retry refetched the expiring source instead of retained bytes")
		}
		before := puts
		_ = ArchiveCanvasImageTask(context.Background(), owner.ID, task.ID)
		if puts != before {
			t.Fatal("completed task uploaded twice")
		}
		object, err := repository.GetStorageObject(strings.TrimPrefix(done.StorageKey, "server:"))
		if err != nil || object.CreatedBy != owner.ID {
			t.Fatal("file owner missing")
		}
		_, found, err := repository.GetUserCanvasImageTask("other", task.ID)
		if err != nil || found {
			t.Fatal("another user read this task")
		}
		otherList, err := repository.ListUserCanvasImageTasks("other", nil, 100)
		if err != nil || len(otherList) != 0 {
			t.Fatal("archived result leaked through recovery listing")
		}
		before = puts
		_ = ArchiveCanvasImageTask(context.Background(), "other", task.ID)
		if puts != before {
			t.Fatal("another user archived this task")
		}
	})

	t.Run("partial multi-image retry preserves GIF frames and avoids duplicate uploads", func(t *testing.T) {
		palette := color.Palette{color.Black, color.White}
		one := image.NewPaletted(image.Rect(0, 0, 1, 1), palette)
		two := image.NewPaletted(image.Rect(0, 0, 1, 1), palette)
		two.Pix[0] = 1
		var animated bytes.Buffer
		if err := gif.EncodeAll(&animated, &gif.GIF{Image: []*image.Paletted{one, two}, Delay: []int{5, 5}}); err != nil {
			t.Fatal(err)
		}
		gifURL := "data:image/gif;base64," + base64.StdEncoding.EncodeToString(animated.Bytes())
		task := newTask("archive-multiple", dataURL, gifURL)
		before := puts
		failAt = puts + 2
		if err := ArchiveCanvasImageTask(context.Background(), owner.ID, task.ID); err == nil {
			t.Fatal("partial storage outage hidden")
		}
		firstURL := read(task.ID).ImageStorage[0].URL
		failAt = 0
		if err := ArchiveCanvasImageTask(context.Background(), owner.ID, task.ID); err != nil {
			t.Fatal(err)
		}
		done := read(task.ID)
		if puts-before != 3 || done.ImageStorage[0].URL != firstURL || done.ImageStorage[1].MimeType != "image/gif" {
			t.Fatal("partial retry duplicated output or changed GIF type")
		}
		for _, ref := range done.ImageStorage {
			if strings.Contains(ref.URL, "data:") {
				t.Fatal("raw bytes survived archive")
			}
		}
		object, err := repository.GetStorageObject(strings.TrimPrefix(done.ImageStorage[1].StorageKey, "server:"))
		if err != nil {
			t.Fatal(err)
		}
		var gifBytes []byte
		for path, data := range stored {
			if strings.HasSuffix(path, object.ObjectKey) {
				gifBytes = data
			}
		}
		decoded, err := gif.DecodeAll(bytes.NewReader(gifBytes))
		if err != nil || len(decoded.Image) != 2 || !bytes.Equal(gifBytes, animated.Bytes()) {
			t.Fatal("animation was altered")
		}
	})

	t.Run("archived task remains discoverable after cleanup without a browser", func(t *testing.T) {
		if err := repository.DeleteFinishedCanvasImageTasksBefore(time.Now().Add(time.Hour).Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
		tasks, err := repository.ListUserCanvasImageTasks(owner.ID, []string{"image-workbench"}, 100)
		if err != nil || len(tasks) != 2 {
			t.Fatal("archived result lost from cloud recovery")
		}
		for _, task := range tasks {
			if CanvasImageTaskResponse(task)["imageStorage"] == nil {
				t.Fatal("per-image storage metadata missing")
			}
		}
	})
	t.Run("cancelled task never uploads", func(t *testing.T) {
		task := newTask("archive-cancelled", dataURL)
		db.Model(&model.CanvasImageTask{}).Where("id = ?", task.ID).Update("status", "cancelled")
		before := puts
		if err := ArchiveCanvasImageTask(context.Background(), owner.ID, task.ID); err != nil || puts != before {
			t.Fatal("cancelled task archived")
		}
	})
	_ = deletes
}
