package repository

import (
	"strings"

	"github.com/tigerowo/infinite-canvas/model"
)

func SaveCanvasImageTask(task model.CanvasImageTask) (model.CanvasImageTask, error) {
	db, err := DB()
	if err != nil {
		return task, err
	}
	return task, db.Create(&task).Error
}

func UpdateCanvasImageTask(task model.CanvasImageTask) (model.CanvasImageTask, error) {
	db, err := DB()
	if err != nil {
		return task, err
	}

	return task, db.Model(&model.CanvasImageTask{}).
		Where("user_id = ? AND id = ? AND status <> ?", task.UserID, task.ID, "cancelled").
		Select(
			"Status", "Progress", "ResponseBody", "Error", "ErrorDetail",
			"ImageURL", "ImageURLs", "ImageStorage", "StorageKey", "Width", "Height",
			"MimeType", "Bytes", "UpdatedAt", "StartedAt", "CompletedAt",
		).
		Updates(&task).Error
}

func GetUserCanvasImageTask(userID string, id string) (model.CanvasImageTask, bool, error) {
	db, err := DB()
	if err != nil {
		return model.CanvasImageTask{}, false, err
	}
	var task model.CanvasImageTask
	err = db.First(&task, "user_id = ? AND id = ?", userID, id).Error
	if err != nil {
		return model.CanvasImageTask{}, false, nil
	}
	return task, true, nil
}

func ListUserCanvasImageTasks(userID string, sources []string, limit int) ([]model.CanvasImageTask, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}
	var tasks []model.CanvasImageTask
	query := db.Where("user_id = ?", userID)
	if len(sources) > 0 {
		query = query.Where("source IN ?", sources)
	}
	err = query.
		Where("(status IN ? OR (status = ? AND storage_key <> ''))", []string{"queued", "processing", "running", "in_progress"}, "completed").
		Order("created_at DESC").
		Limit(limit).
		Find(&tasks).Error
	return tasks, err
}

func BatchUserCanvasImageTasks(userID string, ids []string) ([]model.CanvasImageTask, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	keys := uniqueTrimmedValues(ids...)
	if len(keys) == 0 {
		return []model.CanvasImageTask{}, nil
	}
	var tasks []model.CanvasImageTask
	err = db.Where("user_id = ? AND id IN ?", userID, keys).Find(&tasks).Error
	return tasks, err
}

func DeleteUserCanvasImageTask(userID string, id string) error {
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Where("user_id = ? AND id = ?", userID, strings.TrimSpace(id)).Delete(&model.CanvasImageTask{}).Error
}

func HasActiveCanvasImageTasks() (bool, error) {
	db, err := DB()
	if err != nil {
		return false, err
	}
	var task model.CanvasImageTask
	query := db.Model(&model.CanvasImageTask{}).Select("id").
		Where("status IN ?", []string{"queued", "processing", "running", "in_progress"}).
		Limit(1).
		Find(&task)
	return query.RowsAffected > 0, query.Error
}

func DeleteFinishedCanvasImageTasksBefore(before string) error {
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Where("completed_at <> ? AND completed_at < ?", "", before).
		Where("status IN ?", []string{"completed", "failed", "cancelled"}).
		Where("storage_key = '' OR storage_key IS NULL").
		Delete(&model.CanvasImageTask{}).Error
}

// PendingCanvasImageArchives also finds generated results left by an interrupted process.
func PendingCanvasImageArchives(before string) ([]model.CanvasImageTask, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	var tasks []model.CanvasImageTask
	err = db.Where("status IN ? AND image_url <> '' AND (storage_key = '' OR storage_key IS NULL) AND updated_at <= ?", []string{"processing", "completed"}, before).
		Order("updated_at ASC").Limit(10).Find(&tasks).Error
	return tasks, err
}

func SaveCanvasImageArchive(task model.CanvasImageTask) (bool, error) {
	db, err := DB()
	if err != nil {
		return false, err
	}
	tx := db.Model(&model.CanvasImageTask{}).Where("user_id = ? AND id = ? AND status IN ?", task.UserID, task.ID, []string{"processing", "completed"}).
		Select("Status", "Progress", "ImageURL", "ImageURLs", "ImageStorage", "StorageKey", "Width", "Height", "MimeType", "Bytes", "ResponseBody", "Error", "ErrorDetail", "UpdatedAt", "CompletedAt").Updates(&task)
	return tx.RowsAffected > 0, tx.Error
}

func uniqueTrimmedValues(values ...string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			result = append(result, value)
			seen[value] = true
		}
	}
	return result
}
