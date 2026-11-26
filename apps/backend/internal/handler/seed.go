package handler

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 内置动作与官方模板放在仓库根的 seed/*.json，只作为数据来源，启动时不自动导入：
// 需要时通过后台的「导入」按钮或 POST /api/admin/v1/import/* 手动推一次。

type importResult struct {
	Created int `json:"created"`
	Skipped int `json:"skipped"`
}

// importExerciseRows / importPlanRows 都是幂等的：撞唯一索引或主键就跳过。
// exercises 靠 name 的唯一索引，plans 靠显式 ID（不带 ID 的每次都会新建一条，
// 这是给"追加新模板"用的，重复导入同一份文件会产生多份）。
func importExerciseRows(rows []Exercise) (importResult, error) {
	var r importResult
	e := db.Transaction(func(tx *gorm.DB) error {
		for i := range rows {
			if rows[i].Name == "" {
				continue
			}
			res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows[i])
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				r.Skipped++
			} else {
				r.Created++
			}
		}
		return nil
	})
	return r, e
}

func importPlanRows(rows []Plan) (importResult, error) {
	var r importResult
	e := db.Transaction(func(tx *gorm.DB) error {
		for i := range rows {
			if rows[i].Name == "" {
				continue
			}
			if rows[i].Revision == 0 {
				rows[i].Revision = 1
			}
			// 跨环境的自增 ID 不可信：导入的模板一律按动作名重新挂接
			if e := linkItemsByName(tx, rows[i].Days); e != nil {
				return e
			}
			res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows[i])
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				r.Skipped++
			} else {
				r.Created++
			}
		}
		return nil
	})
	return r, e
}

// decodeItems 同时接受两种请求体：裸数组 [...] 和 {"items":[...]}。
// 前者方便 curl 直接贴文件内容，后者是后台页面用的格式。
func decodeItems[T any](c *gin.Context) ([]T, error) {
	raw, e := c.GetRawData()
	if e != nil {
		return nil, e
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] == ' ' || raw[i] == '\n' || raw[i] == '\t' || raw[i] == '\r' {
			continue
		}
		if raw[i] == '[' {
			var items []T
			return items, json.Unmarshal(raw, &items)
		}
		break
	}
	var wrap struct{ Items []T }
	e = json.Unmarshal(raw, &wrap)
	return wrap.Items, e
}

// importExercises 是后台「训练资料」的导入入口。
func importExercises(c *gin.Context) {
	if !c.GetBool("admin") {
		fail(c, 403, "需要管理员权限")
		return
	}
	items, e := decodeItems[Exercise](c)
	if e != nil || len(items) == 0 {
		fail(c, 400, "JSON 解析失败或没有数据，需要数组或 {\"items\":[...]}")
		return
	}
	r, e := importExerciseRows(items)
	if e != nil {
		save(c, nil, e)
		return
	}
	c.JSON(200, gin.H{"created": r.Created, "skipped": r.Skipped, "total": len(items)})
}

// importPlans 是后台「训练模板」的导入入口。
func importPlans(c *gin.Context) {
	if !c.GetBool("admin") {
		fail(c, 403, "需要管理员权限")
		return
	}
	items, e := decodeItems[Plan](c)
	if e != nil || len(items) == 0 {
		fail(c, 400, "JSON 解析失败或没有数据，需要数组或 {\"items\":[...]}")
		return
	}
	r, e := importPlanRows(items)
	if e != nil {
		save(c, nil, e)
		return
	}
	c.JSON(200, gin.H{"created": r.Created, "skipped": r.Skipped, "total": len(items)})
}

// 导出的 JSON 和 seed/*.json 结构完全一致，可以直接拿去另一个环境导入。
// 文件名带日期，方便区分版本。
func exportFile(c *gin.Context, name string, rows any) {
	c.Header("Content-Disposition",
		fmt.Sprintf(`attachment; filename="%s-%s.json"`, name, time.Now().Format("20060102")))
	c.JSON(200, rows)
}

// exportExercises 导出全部动作，按 ID 升序，保证两次导出结果稳定可比。
func exportExercises(c *gin.Context) {
	if !c.GetBool("admin") {
		fail(c, 403, "需要管理员权限")
		return
	}
	var rows []Exercise
	if e := db.Order("id").Find(&rows).Error; e != nil {
		save(c, nil, e)
		return
	}
	exportFile(c, "exercises", rows)
}

// exportPlans 只导出官方模板（owner=0）。owner 非 0 的是用户自己存的计划，
// 不属于运营数据，导出会带出一堆用户数据，没有意义。
// 加 ?all=1 可以连用户计划一起导（排查问题时偶尔要用）。
func exportPlans(c *gin.Context) {
	if !c.GetBool("admin") {
		fail(c, 403, "需要管理员权限")
		return
	}
	q := db.Order("id")
	if c.Query("all") != "1" {
		q = q.Where("owner = 0")
	}
	var rows []Plan
	if e := q.Find(&rows).Error; e != nil {
		save(c, nil, e)
		return
	}
	exportFile(c, "plans", rows)
}
