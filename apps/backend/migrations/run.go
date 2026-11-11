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
			// 每次查询用独立会话：conn 是连接池上固定的会话，直接链式调用会把
			// Where 条件累加进去（name='001' AND name='002'），导致第二条起的
			// 迁移永远被判定为未执行而重复执行。
			if e := conn.Session(&gorm.Session{}).Table("schema_versions").Where("name=?", entry.Name()).Count(&count).Error; e != nil {
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
