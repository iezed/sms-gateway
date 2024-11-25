package auth

import (
	"github.com/android-sms-gateway/server/internal/sms-gateway/models"
	"gorm.io/gorm"
)

type repository struct {
	db *gorm.DB
}

func newRepository(db *gorm.DB) *repository {
	return &repository{
		db: db,
	}
}

func (r *repository) GetByLogin(login string) (models.User, error) {
	user := models.User{}

	return user, r.db.Where("id = ?", login).Take(&user).Error
}

func (r *repository) Insert(user *models.User) error {
	return r.db.Create(user).Error
}