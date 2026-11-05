package migrations

import (
	"embed"
	"errors"
	"gorm.io/gorm"
	"strings"
)

//go:embed *.sql
var files embed.FS

func Run(db *gorm.DB) error {
	return db.Connection(func(conn *gorm.DB) error {
		var acquired int
		if err := conn.Raw("SELECT GET_LOCK('gym_schema',30)").Scan(&acquired).Error; err != nil {
			return err
		}
		if acquired != 1 {
			return errors.New("无法取得数据库迁移锁")
		}
		defer conn.Exec("SELECT RELEASE_LOCK('gym_schema')")
		if err := conn.Exec("CREATE TABLE IF NOT EXISTS schema_versions (name VARCHAR(128) PRIMARY KEY)").Error; err != nil {
			return err
		}
		entries, e := files.ReadDir(".")
		if e != nil {
			return e
		}
		for _, entry := range entries {
			var count int64
			if e := conn.Table("schema_versions").Where("name=?", entry.Name()).Count(&count).Error; e != nil {
				return e
			}
			if count > 0 {
				continue
			}
			data, e := files.ReadFile(entry.Name())
			if e != nil {
				return e
			}
			for _, statement := range strings.Split(string(data), ";") {
				if strings.TrimSpace(statement) != "" {
					if err := conn.Exec(statement).Error; err != nil {
						return err
					}
				}
			}
			if err := conn.Exec("INSERT INTO schema_versions(name) VALUES (?)", entry.Name()).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
