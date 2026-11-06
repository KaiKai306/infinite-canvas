package handler

import (
	"github.com/tigerowo/infinite-canvas/repository"
	"github.com/tigerowo/infinite-canvas/service"
	"net/http"
	"strings"
)

func UserMediaWorks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	user, _ := service.UserFromContext(r.Context())
	items, total, err := repository.ListMediaWorks(user.ID, parseQuery(r))
	if err != nil {
		FailError(w, err)
		return
	}
	result := []map[string]any{}
	for _, item := range items {
		result = append(result, service.MediaWorkResponse(item))
	}
	OK(w, map[string]any{"items": result, "total": total})
}

func RemoveUserMediaWork(w http.ResponseWriter, r *http.Request, id string) {
	user, _ := service.UserFromContext(r.Context())
	if err := repository.RemoveMediaWork(user.ID, id, taskTime()); err != nil {
		FailError(w, err)
		return
	}
	OK(w, true)
}

func UserMediaWorkContent(w http.ResponseWriter, r *http.Request, id string, thumbnail bool) {
	user, _ := service.UserFromContext(r.Context())
	item, err := repository.GetUserMediaWork(user.ID, id)
	if err != nil {
		FailWithStatus(w, 404, "作品不存在")
		return
	}
	objectID := item.ObjectID
	if thumbnail {
		objectID = item.ThumbnailID
	}
	if objectID == "" {
		FailWithStatus(w, 404, "封面尚未生成")
		return
	}
	object, err := service.StorageObjectInfo(r.Context(), objectID)
	if err != nil || object.CreatedBy != user.ID {
		FailWithStatus(w, 404, "文件不存在")
		return
	}
	if thumbnail && !strings.HasPrefix(object.MimeType, "image/") {
		FailWithStatus(w, 404, "封面不存在")
		return
	}
	FileContent(w, r, objectID)
}
