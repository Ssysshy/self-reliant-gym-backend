package handler

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type TemplateDraft struct {
	ID   uint `gorm:"primaryKey;autoIncrement:false"`
	Name string
	Days []Day `gorm:"serializer:json;type:json"`
}
type Audit struct {
	ID            uint
	Actor, Action string
	Target        uint
	CreatedAt     time.Time
}

func templateEditor(g *gin.RouterGroup) {
	g.GET("/templates", func(c *gin.Context) {
		var plans []Plan
		var drafts []TemplateDraft
		if e := db.Where("owner=0").Find(&plans).Error; e != nil {
			save(c, nil, e)
			return
		}
		if e := db.Find(&drafts).Error; e != nil {
			save(c, nil, e)
			return
		}
		byID := map[uint]TemplateDraft{}
		for _, d := range drafts {
			byID[d.ID] = d
		}
		for i, p := range plans {
			if d, ok := byID[p.ID]; ok {
				plans[i].Days = d.Days
				plans[i].Name = d.Name
			}
		}
		c.JSON(200, plans)
	})
	g.POST("/templates", func(c *gin.Context) {
		var input struct {
			Plan
			Intent string
		}
		if c.ShouldBindJSON(&input) != nil || input.Name == "" || (input.Intent != "draft" && input.Intent != "publish" && input.Intent != "withdraw") {
			fail(c, 400, "模板操作无效")
			return
		}
		if input.Intent == "publish" && !valid(input.Plan) {
			fail(c, 400, "请检查训练日、组次与休息时长")
			return
		}
		e := db.Transaction(func(tx *gorm.DB) error {
			var p Plan
			if input.ID != 0 {
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("owner=0").First(&p, input.ID).Error; err != nil {
					return err
				}
				if p.Revision != input.Revision {
					return conflict
				}
			}
			if input.Intent == "publish" {
				var names []string
				if err := tx.Model(&Exercise{}).Pluck("name", &names).Error; err != nil {
					return err
				}
				known := map[string]bool{}
				for _, name := range names {
					known[name] = true
				}
				for _, d := range input.Days {
					for _, i := range d.Items {
						if !known[i.Name] {
							return conflict
						}
					}
				}
				p.Name = input.Name
				p.Days = input.Days
				p.Published = true
			} else if input.Intent == "withdraw" {
				p.Published = false
			} else if p.ID == 0 {
				p.Name = input.Name
			}
			p.Revision++
			if err := tx.Save(&p).Error; err != nil {
				return err
			}
			if input.Intent == "draft" {
				if err := tx.Save(&TemplateDraft{ID: p.ID, Name: input.Name, Days: input.Days}).Error; err != nil {
					return err
				}
			} else if input.Intent == "publish" {
				if err := tx.Delete(&TemplateDraft{}, p.ID).Error; err != nil {
					return err
				}
			}
			return tx.Create(&Audit{Actor: c.GetString("actor"), Action: input.Intent, Target: p.ID}).Error
		})
		result(c, true, e)
	})
}
