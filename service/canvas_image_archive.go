package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
)

const generatedImageLimit = 128 << 20

var imageArchiveRuns sync.Map
var imageArchiveWorkerOnce sync.Once

// A failed upload stays recoverable in the task row. Retrying never calls the model again.
func StartCanvasImageArchiveWorker() {
	if !config.Cfg.ServerImageArchive {
		return
	}
	imageArchiveWorkerOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				tasks, err := repository.PendingCanvasImageArchives(time.Now().Add(-time.Minute).Format(time.RFC3339))
				if err != nil {
					log.Printf("read pending image archives failed")
				}
				for _, task := range tasks {
					ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
					err := ArchiveCanvasImageTask(ctx, task.UserID, task.ID)
					cancel()
					if err != nil {
						log.Printf("image archive pending task=%s err=%v", task.ID, err)
					}
				}
				<-ticker.C
			}
		}()
	})
}

func ArchiveCanvasImageTask(ctx context.Context, userID, id string) error {
	key := struct{ userID, id string }{userID, id}
	if _, busy := imageArchiveRuns.LoadOrStore(key, true); busy {
		return nil
	}
	defer imageArchiveRuns.Delete(key)
	task, found, err := repository.GetUserCanvasImageTask(userID, id)
	if err != nil {
		return errors.New("读取图片归档任务失败")
	}
	if !found || task.StorageKey != "" || task.ImageURL == "" || (task.Status != "processing" && task.Status != "completed") {
		return nil
	}
	user, found, err := repository.GetUserByID(userID)
	if err != nil || !found || user.Role == model.UserRoleGuest {
		return errors.New("图片归档用户不存在")
	}
	ctx = WithUser(ctx, model.PublicUser(user))
	urls := append([]string(nil), task.ImageURLs...)
	if len(urls) == 0 {
		urls = []string{task.ImageURL}
	}
	if len(task.ImageStorage) != len(urls) {
		task.ImageStorage = make([]*model.CanvasTaskImage, len(urls))
	}
	task.Status, task.Progress, task.CompletedAt = "processing", 95, ""
	for index, source := range urls {
		if task.ImageStorage[index] != nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		data, contentType, err := generatedImageBytes(ctx, source)
		if err != nil {
			return err
		}
		// Persist expiring remote URLs as bytes before upload, so a storage outage does not lose a paid result.
		if !strings.HasPrefix(source, "data:") {
			urls[index] = "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data)
			task.ImageURL = urls[0]
			if len(urls) > 1 {
				task.ImageURLs = urls
			}
			if err := saveImageArchive(task); err != nil {
				return err
			}
		}
		extensions, _ := mime.ExtensionsByType(contentType)
		ext := ".png"
		if len(extensions) > 0 {
			ext = extensions[0]
		}
		stored, err := UploadStorageObject(ctx, "generated"+ext, contentType, data)
		if err != nil {
			_ = saveImageArchive(task) // timestamp spaces retries; raw result remains available to the worker.
			return errors.New("对象存储暂不可用，生成结果已保留，后台将重试归档")
		}
		latest, found, loadErr := repository.GetUserCanvasImageTask(userID, id)
		if loadErr != nil || !found || latest.Status == "cancelled" {
			_ = DeleteStorageObject(ctx, stored.ID, nil)
			return context.Canceled
		}
		ref := &model.CanvasTaskImage{URL: stored.URL, StorageKey: stored.StorageKey, MimeType: stored.MimeType, Bytes: stored.Bytes}
		if dimensions, _, decodeErr := image.DecodeConfig(bytes.NewReader(data)); decodeErr == nil {
			ref.Width, ref.Height = dimensions.Width, dimensions.Height
		}
		task.ImageStorage[index], urls[index] = ref, stored.URL
		task.ImageURL = urls[0]
		if len(urls) > 1 {
			task.ImageURLs = urls
		}
		if err := saveImageArchive(task); err != nil {
			_ = DeleteStorageObject(ctx, stored.ID, nil)
			return err
		}
	}
	first := task.ImageStorage[0]
	task.ImageURL, task.StorageKey = first.URL, first.StorageKey
	task.MimeType, task.Bytes, task.Width, task.Height = first.MimeType, first.Bytes, first.Width, first.Height
	task.Status, task.Progress, task.CompletedAt = "completed", 100, now()
	task.ResponseBody, task.Error, task.ErrorDetail = "", "", ""
	return saveImageArchive(task)
}

func saveImageArchive(task model.CanvasImageTask) error {
	task.UpdatedAt = now()
	saved, err := repository.SaveCanvasImageArchive(task)
	if err != nil {
		return errors.New("保存图片归档状态失败")
	}
	if !saved {
		return context.Canceled
	}
	return nil
}

func generatedImageBytes(ctx context.Context, source string) ([]byte, string, error) {
	var data []byte
	if strings.HasPrefix(source, "data:") {
		header, payload, ok := strings.Cut(source, ",")
		if !ok || !strings.HasSuffix(header, ";base64") || base64.StdEncoding.DecodedLen(len(payload)) > generatedImageLimit {
			return nil, "", errors.New("生成图片数据无效或超过大小限制")
		}
		var err error
		data, err = base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return nil, "", errors.New("生成图片编码无效")
		}
	} else {
		if !strings.HasPrefix(source, "https://") && !strings.HasPrefix(source, "http://") {
			return nil, "", errors.New("生成图片地址无效")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if err != nil {
			return nil, "", errors.New("生成图片地址无效")
		}
		response, err := SafeProxyHTTPClient().Do(req)
		if err != nil {
			return nil, "", errors.New("获取生成图片失败")
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return nil, "", errors.New("生成图片地址暂不可用")
		}
		data, err = io.ReadAll(io.LimitReader(response.Body, generatedImageLimit+1))
		if err != nil || len(data) > generatedImageLimit {
			return nil, "", errors.New("生成图片读取失败或超过大小限制")
		}
	}
	contentType := http.DetectContentType(data)
	if len(data) == 0 || !strings.HasPrefix(contentType, "image/") {
		return nil, "", errors.New("生成结果不是有效图片")
	}
	return data, contentType, nil
}
