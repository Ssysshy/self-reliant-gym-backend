package handler

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 动作资料的后台写操作：新增/编辑（含改名）、删除、删除前的影响面查询。
// 读接口在 catalog.go，这里只放会改动数据或引用关系的。

var dupExerciseName = errors.New("已有同名动作")

func parseExerciseID(c *gin.Context) (uint, bool) {
	id, e := strconv.ParseUint(c.Param("id"), 10, 32)
	if e != nil || id == 0 {
		fail(c, 400, "动作无效")
		return 0, false
	}
	return uint(id), true
}

func exerciseAdminRoutes(g *gin.RouterGroup) {
	g.POST("/exercises", saveExercise)
	g.GET("/exercises/:id/references", exerciseReferences)
	g.DELETE("/exercises/:id", deleteExercise)
}

// saveExercise 同时处理新增和编辑。
// 改名不再拦截：引用侧靠 ExerciseID 定位，名称只是一次事务里的快照刷新。
func saveExercise(c *gin.Context) {
	var x Exercise
	if c.ShouldBindJSON(&x) != nil || strings.TrimSpace(x.Name) == "" || x.Guide == "" || x.Equipment == "" || x.Setup == "" {
		fail(c, 400, "请填写完整资料")
		return
	}
	x.Name = strings.TrimSpace(x.Name)
	if msg := taxonomy(x); msg != "" {
		fail(c, 400, msg)
		return
	}
	if !imagePath(x.ImageURL) {
		fail(c, 400, "示范图请填相对路径或 https 地址")
		return
	}
	var moved struct{ Templates, Plans, Drafts int }
	e := db.Transaction(func(tx *gorm.DB) error {
		if x.ID == 0 {
			var n int64
			if e := tx.Model(&Exercise{}).Where("name=?", x.Name).Count(&n).Error; e != nil {
				return e
			}
			if n > 0 {
				return dupExerciseName
			}
			return tx.Create(&x).Error
		}
		var old Exercise
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&old, x.ID).Error; e != nil {
			return e
		}
		if old.Name != x.Name {
			var n int64
			if e := tx.Model(&Exercise{}).Where("name=? AND id<>?", x.Name, x.ID).Count(&n).Error; e != nil {
				return e
			}
			if n > 0 {
				return dupExerciseName
			}
		}
		// Save 会写全部字段，空字符串也能清掉（ImageURL / AuxMuscles 允许留空）
		if e := tx.Save(&x).Error; e != nil {
			return e
		}
		if old.Name == x.Name {
			return nil
		}
		t, p, d, e := renameExerciseRefs(tx, x.ID, x.Name)
		if e != nil {
			return e
		}
		moved.Templates, moved.Plans, moved.Drafts = t, p, d
		return tx.Create(&Audit{
			Actor:  c.GetString("actor"),
			Action: fmt.Sprintf("动作改名「%s」→「%s」：同步模板 %d、个人计划 %d、模板草稿 %d", old.Name, x.Name, t, p, d),
			Target: x.ID,
		}).Error
	})
	if errors.Is(e, dupExerciseName) || (e != nil && strings.Contains(e.Error(), "Duplicate entry")) {
		fail(c, 409, "已有同名动作，请先合并或换一个名称")
		return
	}
	result(c, x, e)
}

// exerciseReferences 给删除弹窗用：只统计，不改数据。
func exerciseReferences(c *gin.Context) {
	id, ok := parseExerciseID(c)
	if !ok {
		return
	}
	var x Exercise
	if e := db.Select("id").First(&x, id).Error; e != nil {
		save(c, nil, e)
		return
	}
	out, e := collectExerciseImpact(db, id)
	result(c, out, e)
}

// deleteExercise 删除动作：引用项降级成自建动作（ID 置 0、名称保留），
// 计划/模板照常能练，只是失去教学详情；训练记录本来就不级联。
// 需要管理员：这一步影响面比改一个字段大。
func deleteExercise(c *gin.Context) {
	if !c.GetBool("admin") {
		fail(c, 403, "需要管理员权限")
		return
	}
	id, ok := parseExerciseID(c)
	if !ok {
		return
	}
	var x Exercise
	if e := db.First(&x, id).Error; e != nil {
		save(c, nil, e)
		return
	}
	var out struct{ Templates, Plans, Drafts int }
	e := db.Transaction(func(tx *gorm.DB) error {
		t, p, d, e := detachExerciseRefs(tx, x.ID)
		if e != nil {
			return e
		}
		out.Templates, out.Plans, out.Drafts = t, p, d
		if e := tx.Delete(&Exercise{}, x.ID).Error; e != nil {
			return e
		}
		return tx.Create(&Audit{
			Actor:  c.GetString("actor"),
			Action: fmt.Sprintf("删除动作「%s」：模板 %d、个人计划 %d、草稿 %d 转为自建动作", x.Name, t, p, d),
			Target: x.ID,
		}).Error
	})
	result(c, out, e)
}
