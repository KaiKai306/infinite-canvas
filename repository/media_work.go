package repository

import (
	"github.com/tigerowo/infinite-canvas/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func SaveMediaWorksAndMarkTask(task model.CanvasImageTask, items []model.MediaWork, at string) error {
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if len(items) > 0 {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(items, 50).Error; err != nil {
				return err
			}
		}
		return tx.Model(&model.CanvasImageTask{}).Where("user_id = ? AND id = ? AND status = 'completed'", task.UserID, task.ID).Update("works_indexed_at", at).Error
	})
}

func UnindexedImageWorkTasks() ([]model.CanvasImageTask, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	var items []model.CanvasImageTask
	err = db.Where("status = 'completed' AND storage_key <> '' AND (works_indexed_at = '' OR works_indexed_at IS NULL)").Order("created_at ASC").Limit(30).Find(&items).Error
	return items, err
}

func UnindexedCloudVideos() ([]model.StorageObject, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	var items []model.StorageObject
	err = db.Where("mime_type LIKE 'video/%' AND direct = ? AND EXISTS (SELECT 1 FROM users WHERE users.id = storage_objects.created_by) AND NOT EXISTS (SELECT 1 FROM media_works WHERE media_works.object_id = storage_objects.id)", false).Order("created_at ASC").Limit(30).Find(&items).Error
	return items, err
}

func SaveMediaWork(item model.MediaWork) error {
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&item).Error
}

func ListMediaWorks(userID string, q model.Query) ([]model.MediaWork, int64, error) {
	db, err := DB()
	if err != nil {
		return nil, 0, err
	}
	q.Normalize()
	if q.PageSize > 100 {
		q.PageSize = 100
	}
	query := db.Model(&model.MediaWork{}).Where("user_id = ? AND deleted_at = ''", userID)
	if q.Type == "image" || q.Type == "video" {
		query = query.Where("kind = ?", q.Type)
	}
	if q.Keyword != "" {
		like := "%" + q.Keyword + "%"
		query = query.Where("(title LIKE ? OR prompt LIKE ? OR model LIKE ?)", like, like, like)
	}
	var total int64
	if err = query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := []model.MediaWork{}
	err = query.Order("created_at DESC, id DESC").Offset(q.Offset()).Limit(q.PageSize).Find(&items).Error
	return items, total, err
}

func GetUserMediaWork(userID, id string) (model.MediaWork, error) {
	db, err := DB()
	if err != nil {
		return model.MediaWork{}, err
	}
	var item model.MediaWork
	err = db.Where("user_id = ? AND id = ? AND deleted_at = ''", userID, id).First(&item).Error
	return item, err
}

func RemoveMediaWork(userID, id, at string) error {
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Model(&model.MediaWork{}).Where("user_id = ? AND id = ?", userID, id).Update("deleted_at", at).Error
}

func StorageObjectUsedByWork(id string) (bool, error) {
	db, err := DB()
	if err != nil {
		return false, err
	}
	var count int64
	err = db.Model(&model.MediaWork{}).Where("deleted_at = '' AND (object_id = ? OR thumbnail_id = ?)", id, id).Count(&count).Error
	return count > 0, err
}

func PendingWorkThumbnails(before string) ([]model.MediaWork, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	var items []model.MediaWork
	err = db.Where("kind = 'image' AND deleted_at = '' AND thumbnail_id = '' AND thumbnail_state <> 'unsupported' AND (thumbnail_checked_at = '' OR thumbnail_checked_at <= ?)", before).Order("thumbnail_checked_at ASC").Limit(10).Find(&items).Error
	return items, err
}

func SaveWorkThumbnail(userID, id, objectID, state, at string) (bool, error) {
	db, err := DB()
	if err != nil {
		return false, err
	}
	tx := db.Model(&model.MediaWork{}).Where("user_id = ? AND id = ? AND deleted_at = '' AND thumbnail_id = ''", userID, id).Updates(map[string]any{"thumbnail_id": objectID, "thumbnail_state": state, "thumbnail_checked_at": at})
	return tx.RowsAffected > 0, tx.Error
}
