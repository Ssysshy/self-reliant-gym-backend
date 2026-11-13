package handler

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

func revoke(tx *gorm.DB, g *Grant, at time.Time) error {
	if g.Revoked != nil {
		return nil
	}
	if err := tx.Model(g).Update("revoked", at).Error; err != nil {
		return err
	}
	var active []Grant
	if err := tx.Where("user_id=? AND revoked IS NULL", g.UserID).Order("created_at,id").Find(&active).Error; err != nil {
		return err
	}
	var until time.Time
	for _, v := range active {
		if v.CreatedAt.After(until) {
			until = v.CreatedAt
		}
		until = until.AddDate(0, 0, v.Days)
	}
	if until.IsZero() {
		return tx.Model(&User{}).Where("id=?", g.UserID).Update("expires", nil).Error
	}
	return tx.Model(&User{}).Where("id=?", g.UserID).Update("expires", until).Error
}
func staffGrants(g *gin.RouterGroup) {
	g.GET("/audit", func(c *gin.Context) {
		var rows []Audit
		e := db.Order("id desc").Limit(200).Find(&rows).Error
		save(c, rows, e)
	})
	g.GET("/members/:id/history", func(c *gin.Context) {
		var rows []Grant
		e := db.Where("user_id=?", c.Param("id")).Order("id desc").Find(&rows).Error
		save(c, rows, e)
	})
	g.POST("/members/:id/grants/:grant/revoke", func(c *gin.Context) {
		if !c.GetBool("admin") {
			fail(c, 403, "需要管理员权限")
			return
		}
		var input struct{ Reason string }
		if c.ShouldBindJSON(&input) != nil || input.Reason == "" {
			fail(c, 400, "请输入撤销原因")
			return
		}
		e := db.Transaction(func(tx *gorm.DB) error {
			var u User
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&u, "id=?", c.Param("id")).Error; err != nil {
				return err
			}
			var grant Grant
			if err := tx.Where("user_id=? AND operator=?", u.ID, "admin").First(&grant, "id=?", c.Param("grant")).Error; err != nil {
				return err
			}
			if grant.Revoked != nil {
				return nil
			}
			if err := revoke(tx, &grant, time.Now()); err != nil {
				return err
			}
			return tx.Create(&Audit{Actor: c.GetString("actor"), Action: "撤销权益：" + input.Reason, Target: grant.ID}).Error
		})
		result(c, true, e)
	})
}
