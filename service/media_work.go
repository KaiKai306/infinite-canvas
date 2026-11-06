package service

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	stddraw "image/draw"
	"image/jpeg"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"gorm.io/gorm"
)

var mediaWorkWorkerOnce sync.Once
var mediaWorkRuns sync.Map

func workForObject(object model.StorageObject) model.MediaWork {
	kind := "image"
	if strings.HasPrefix(object.MimeType, "video/") {
		kind = "video"
	}
	return model.MediaWork{ID: "work-" + object.ID, UserID: object.CreatedBy, ObjectID: object.ID, Kind: kind, Title: "生成图片", Width: object.Width, Height: object.Height, Bytes: object.Bytes, MimeType: object.MimeType, CreatedAt: object.CreatedAt, ThumbnailState: "pending"}
}

func IndexArchivedImageTask(task model.CanvasImageTask) error {
	if task.Status != "completed" || task.StorageKey == "" {
		return nil
	}
	refs := task.ImageStorage
	if len(refs) == 0 {
		refs = []*model.CanvasTaskImage{{StorageKey: task.StorageKey, Width: task.Width, Height: task.Height, MimeType: task.MimeType, Bytes: task.Bytes}}
	}
	items := []model.MediaWork{}
	for _, ref := range refs {
		if ref == nil || !strings.HasPrefix(ref.StorageKey, "server:") {
			continue
		}
		object, err := repository.GetStorageObject(strings.TrimPrefix(ref.StorageKey, "server:"))
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if object.CreatedBy != task.UserID || object.Direct || !strings.HasPrefix(object.MimeType, "image/") {
			continue
		}
		item := workForObject(object)
		item.Prompt, item.Model, item.Source, item.TaskID = task.Prompt, task.Model, task.Source, task.ID
		item.Width, item.Height = ref.Width, ref.Height
		if title := []rune(strings.TrimSpace(task.Prompt)); len(title) > 0 {
			if len(title) > 64 {
				title = title[:64]
			}
			item.Title = string(title)
		}
		items = append(items, item)
	}
	return repository.SaveMediaWorksAndMarkTask(task, items, now())
}

func StartMediaWorkWorker() {
	if !config.Cfg.ServerImageArchive {
		return
	}
	mediaWorkWorkerOnce.Do(func() {
		if err := repository.MarkInterruptedImageAPIRequests(now()); err != nil {
			log.Printf("image API restart reconciliation pending")
		}
		go func() {
			ticker := time.NewTicker(15 * time.Second)
			defer ticker.Stop()
			for {
				if err := SyncMediaWorks(); err != nil {
					log.Printf("media work indexing pending")
				}
				items, err := repository.PendingWorkThumbnails(time.Now().Add(-time.Minute).Format(time.RFC3339))
				if err != nil {
					log.Printf("media thumbnails pending")
				}
				for _, item := range items {
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
					err := BuildMediaWorkThumbnail(ctx, item)
					cancel()
					if err != nil {
						log.Printf("media thumbnail pending work=%s", item.ID)
					}
				}
				<-ticker.C
			}
		}()
	})
}

func SyncMediaWorks() error {
	tasks, err := repository.UnindexedImageWorkTasks()
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if err := IndexArchivedImageTask(task); err != nil {
			return err
		}
	}
	videos, err := repository.UnindexedCloudVideos()
	if err != nil {
		return err
	}
	for _, object := range videos {
		if object.Direct {
			continue
		}
		if _, found, err := repository.GetUserByID(object.CreatedBy); err != nil || !found {
			continue
		}
		item := workForObject(object)
		item.Title, item.Source, item.ThumbnailState = "云端视频", "cloud-video", "unavailable"
		if err := repository.SaveMediaWork(item); err != nil {
			return err
		}
	}
	return nil
}

func BuildMediaWorkThumbnail(ctx context.Context, item model.MediaWork) error {
	if _, busy := mediaWorkRuns.LoadOrStore(item.ID, true); busy {
		return nil
	}
	defer mediaWorkRuns.Delete(item.ID)
	user, found, err := repository.GetUserByID(item.UserID)
	if err != nil || !found {
		return errors.New("缩略图用户不存在")
	}
	auth := model.PublicUser(user)
	auth.Role = model.UserRoleUser
	ctx = WithUser(ctx, auth)
	if _, err := repository.GetUserMediaWork(item.UserID, item.ID); err != nil {
		return nil
	}
	stream, err := DownloadStorageObject(ctx, item.ObjectID, "")
	if err != nil {
		_, _ = repository.SaveWorkThumbnail(item.UserID, item.ID, "", "pending", now())
		return err
	}
	data, readErr := io.ReadAll(io.LimitReader(stream.Stream, (32<<20)+1))
	stream.Stream.Close()
	if readErr != nil {
		_, _ = repository.SaveWorkThumbnail(item.UserID, item.ID, "", "pending", now())
		return readErr
	}
	preview, err := mediaWorkThumbnail(data)
	if err != nil {
		_, _ = repository.SaveWorkThumbnail(item.UserID, item.ID, "", "unsupported", now())
		return nil
	}
	uploaded, err := UploadStorageObject(ctx, "work-thumbnail.jpg", "image/jpeg", preview)
	if err != nil {
		_, _ = repository.SaveWorkThumbnail(item.UserID, item.ID, "", "pending", now())
		return err
	}
	saved, err := repository.SaveWorkThumbnail(item.UserID, item.ID, uploaded.ID, "ready", now())
	if err != nil || !saved {
		_ = DeleteStorageObject(ctx, uploaded.ID, nil)
	}
	return err
}

func mediaWorkThumbnail(data []byte) ([]byte, error) {
	if len(data) > 32<<20 {
		return nil, errors.New("缩略图原图过大")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 32000000 {
		return nil, errors.New("缩略图尺寸超限")
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	width, height := cfg.Width, cfg.Height
	if width > 512 || height > 512 {
		if width >= height {
			height = max(1, height*512/width)
			width = 512
		} else {
			width = max(1, width*512/height)
			height = 512
		}
	}
	target := image.NewRGBA(image.Rect(0, 0, width, height))
	stddraw.Draw(target, target.Bounds(), image.NewUniform(color.White), image.Point{}, stddraw.Src)
	draw.CatmullRom.Scale(target, target.Bounds(), source, source.Bounds(), stddraw.Over, nil)
	var buffer bytes.Buffer
	err = jpeg.Encode(&buffer, target, &jpeg.Options{Quality: 80})
	return buffer.Bytes(), err
}

func MediaWorkResponse(item model.MediaWork) map[string]any {
	thumbnailURL := ""
	if item.ThumbnailID != "" {
		thumbnailURL = "/api/works/" + item.ID + "/thumbnail"
	}
	return map[string]any{"id": item.ID, "kind": item.Kind, "title": item.Title, "prompt": item.Prompt, "model": item.Model, "source": item.Source, "taskId": item.TaskID, "width": item.Width, "height": item.Height, "bytes": item.Bytes, "mimeType": item.MimeType, "createdAt": item.CreatedAt, "thumbnailState": item.ThumbnailState, "thumbnailUrl": thumbnailURL, "url": "/api/works/" + item.ID + "/content"}
}
