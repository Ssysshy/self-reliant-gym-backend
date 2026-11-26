package handler

import (
	"encoding/json"
	"strings"

	"gorm.io/gorm"
)

// ExerciseID 是模板/计划与动作资料之间唯一的引用键，Name 只是展示快照。
// 这个文件集中处理引用侧的三件事：
//   1. 启动时给存量 JSON 回填 ExerciseID（幂等，只跑一次）；
//   2. 保存计划/模板时按 ID 归一名称（客户端拿旧页面提交也不会把旧名字写回去）；
//   3. 改名、删除动作时同步引用侧。
// 训练记录（sessions.day）是当时的快照，以上任何操作都不碰它。

// backfillMarker 记在 schema_versions 里，表示存量数据已经回填过。
// 不用 SQL 迁移做：days 是 JSON 数组，SQL 里遍历改写又长又容易漏，Go 里可读也可测。
const backfillMarker = "014_item_exercise_id"

func backfillItemExerciseIDs() error {
	var done int64
	if e := db.Table("schema_versions").Where("name=?", backfillMarker).Count(&done).Error; e != nil {
		return e
	}
	if done > 0 {
		return nil
	}
	// 多实例同时启动时只让一个跑；抢不到锁说明别人在回填，结果一样，直接跳过。
	return db.Connection(func(conn *gorm.DB) error {
		var got int
		if e := conn.Raw("SELECT GET_LOCK('gym_item_refs',30)").Scan(&got).Error; e != nil {
			return e
		}
		if got != 1 {
			return nil
		}
		defer conn.Exec("SELECT RELEASE_LOCK('gym_item_refs')")
		// 拿到锁后再查一次：可能在等锁的这段时间里另一个实例已经做完了。
		var again int64
		if e := conn.Session(&gorm.Session{NewDB: true}).Table("schema_versions").Where("name=?", backfillMarker).Count(&again).Error; e != nil {
			return e
		}
		if again > 0 {
			return nil
		}
		return conn.Session(&gorm.Session{NewDB: true}).Transaction(func(tx *gorm.DB) error {
			var rows []Exercise
			if e := tx.Select("id", "name").Find(&rows).Error; e != nil {
				return e
			}
			byName := map[string]uint{}
			for _, x := range rows {
				byName[x.Name] = x.ID
			}
			var plans []Plan
			if e := tx.Select("id", "days").Find(&plans).Error; e != nil {
				return e
			}
			for _, p := range plans {
				if !fillMissingIDs(p.Days, byName) {
					continue
				}
				if e := saveDays(tx, "plans", p.ID, p.Days); e != nil {
					return e
				}
			}
			var drafts []TemplateDraft
			if e := tx.Select("id", "days").Find(&drafts).Error; e != nil {
				return e
			}
			for _, d := range drafts {
				if !fillMissingIDs(d.Days, byName) {
					continue
				}
				if e := saveDays(tx, "template_drafts", d.ID, d.Days); e != nil {
					return e
				}
			}
			return tx.Exec("INSERT IGNORE INTO schema_versions(name) VALUES (?)", backfillMarker).Error
		})
	})
}

// fillMissingIDs 只补 ID=0 的条目，已有 ID 的一律不动。
// 名字在动作库里找不到的保持 0，等价于「自建动作」，不报错。
func fillMissingIDs(days []Day, byName map[string]uint) bool {
	changed := false
	for di := range days {
		for ii := range days[di].Items {
			it := &days[di].Items[ii]
			if it.ExerciseID != 0 {
				continue
			}
			if id, ok := byName[strings.TrimSpace(it.Name)]; ok {
				it.ExerciseID = id
				changed = true
			}
		}
	}
	return changed
}

// normalizeItems 是保存计划/模板前的统一入口：
//   - ID 非 0：名称以动作资料为准，动作被删了就降级成自建（ID=0，名字保留）；
//   - ID 为 0：按名称兜底匹配一次（兼容没传 ID 的老版本小程序），匹配不到就是自建。
//
// 只查当前计划里出现过的 ID / 名称，不会全表扫动作库。
func normalizeItems(tx *gorm.DB, days []Day) error {
	ids, names := collectItemRefs(days)
	byID := map[uint]string{}
	byName := map[string]uint{}
	if len(ids) > 0 {
		var rows []Exercise
		if e := tx.Select("id", "name").Where("id IN ?", ids).Find(&rows).Error; e != nil {
			return e
		}
		for _, r := range rows {
			byID[r.ID] = r.Name
			byName[r.Name] = r.ID
		}
	}
	if len(names) > 0 {
		var rows []Exercise
		if e := tx.Select("id", "name").Where("name IN ?", names).Find(&rows).Error; e != nil {
			return e
		}
		for _, r := range rows {
			byName[r.Name] = r.ID
		}
	}
	for di := range days {
		for ii := range days[di].Items {
			it := &days[di].Items[ii]
			it.Name = strings.TrimSpace(it.Name)
			if it.ExerciseID != 0 {
				if name, ok := byID[it.ExerciseID]; ok {
					it.Name = name
				} else {
					it.ExerciseID = 0
				}
				continue
			}
			if it.Name == "" {
				continue
			}
			if id, ok := byName[it.Name]; ok {
				it.ExerciseID = id
			}
		}
	}
	return nil
}

// linkItemsByName 给导入用：跨环境的自增 ID 不可信，一律按名称重新挂接。
func linkItemsByName(tx *gorm.DB, days []Day) error {
	for di := range days {
		for ii := range days[di].Items {
			days[di].Items[ii].ExerciseID = 0
		}
	}
	return normalizeItems(tx, days)
}

func collectItemRefs(days []Day) (ids []uint, names []string) {
	seenID := map[uint]bool{}
	seenName := map[string]bool{}
	for _, d := range days {
		for _, i := range d.Items {
			name := strings.TrimSpace(i.Name)
			if i.ExerciseID != 0 {
				if !seenID[i.ExerciseID] {
					seenID[i.ExerciseID] = true
					ids = append(ids, i.ExerciseID)
				}
				continue
			}
			if name != "" && !seenName[name] {
				seenName[name] = true
				names = append(names, name)
			}
		}
	}
	return
}

// saveDays 直接写 JSON 列：GORM 的 serializer 只保证结构体字段读写，
// 用 Exec 传序列化后的字节更明确，也避免 Save 把没查出来的列覆盖成零值。
func saveDays(tx *gorm.DB, table string, id uint, days []Day) error {
	raw, e := json.Marshal(days)
	if e != nil {
		return e
	}
	return tx.Exec("UPDATE "+table+" SET days=? WHERE id=?", raw, id).Error
}

func hasItemRef(days []Day, id uint) bool {
	for _, d := range days {
		for _, i := range d.Items {
			if i.ExerciseID == id {
				return true
			}
		}
	}
	return false
}

func renameInDays(days []Day, id uint, name string) bool {
	changed := false
	for di := range days {
		for ii := range days[di].Items {
			it := &days[di].Items[ii]
			if it.ExerciseID == id && it.Name != name {
				it.Name = name
				changed = true
			}
		}
	}
	return changed
}

func detachInDays(days []Day, id uint) bool {
	changed := false
	for di := range days {
		for ii := range days[di].Items {
			it := &days[di].Items[ii]
			if it.ExerciseID == id {
				it.ExerciseID = 0
				changed = true
			}
		}
	}
	return changed
}

// renameExerciseRefs 把引用这个动作的模板/草稿/个人计划里的名称快照改成新名字。
// 训练记录不在这里：历史就该记当时那个名字。
func renameExerciseRefs(tx *gorm.DB, id uint, name string) (templates, plans, drafts int, e error) {
	var rows []Plan
	if e = tx.Select("id", "owner", "days").Find(&rows).Error; e != nil {
		return
	}
	for _, p := range rows {
		if !renameInDays(p.Days, id, name) {
			continue
		}
		if e = saveDays(tx, "plans", p.ID, p.Days); e != nil {
			return
		}
		if p.Owner == 0 {
			templates++
		} else {
			plans++
		}
	}
	var list []TemplateDraft
	if e = tx.Select("id", "days").Find(&list).Error; e != nil {
		return
	}
	for _, d := range list {
		if !renameInDays(d.Days, id, name) {
			continue
		}
		if e = saveDays(tx, "template_drafts", d.ID, d.Days); e != nil {
			return
		}
		drafts++
	}
	return
}

// detachExerciseRefs 删除动作时把引用项降级成自建动作：ID 置 0，名称保留，
// 计划/模板照常能练，只是点不进教学详情。
func detachExerciseRefs(tx *gorm.DB, id uint) (templates, plans, drafts int, e error) {
	var rows []Plan
	if e = tx.Select("id", "owner", "days").Find(&rows).Error; e != nil {
		return
	}
	for _, p := range rows {
		if !detachInDays(p.Days, id) {
			continue
		}
		if e = saveDays(tx, "plans", p.ID, p.Days); e != nil {
			return
		}
		if p.Owner == 0 {
			templates++
		} else {
			plans++
		}
	}
	var list []TemplateDraft
	if e = tx.Select("id", "days").Find(&list).Error; e != nil {
		return
	}
	for _, d := range list {
		if !detachInDays(d.Days, id) {
			continue
		}
		if e = saveDays(tx, "template_drafts", d.ID, d.Days); e != nil {
			return
		}
		drafts++
	}
	return
}

type exerciseRefTemplate struct {
	ID        uint
	Name      string
	Published bool
}

// exerciseImpact 是删除前给后台看的影响面。训练记录不列：它不会被改动。
type exerciseImpact struct {
	Templates []exerciseRefTemplate
	Drafts    int
	Plans     int
}

func collectExerciseImpact(tx *gorm.DB, id uint) (exerciseImpact, error) {
	out := exerciseImpact{Templates: []exerciseRefTemplate{}}
	var rows []Plan
	if e := tx.Select("id", "owner", "name", "published", "days").Find(&rows).Error; e != nil {
		return out, e
	}
	for _, p := range rows {
		if !hasItemRef(p.Days, id) {
			continue
		}
		if p.Owner == 0 {
			out.Templates = append(out.Templates, exerciseRefTemplate{ID: p.ID, Name: p.Name, Published: p.Published})
		} else {
			out.Plans++
		}
	}
	var drafts []TemplateDraft
	if e := tx.Select("id", "days").Find(&drafts).Error; e != nil {
		return out, e
	}
	for _, d := range drafts {
		if hasItemRef(d.Days, id) {
			out.Drafts++
		}
	}
	return out, nil
}
