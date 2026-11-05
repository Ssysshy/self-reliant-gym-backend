package handler

import (
	"errors"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"log"
	"strings"
	"time"
)

var conflict = errors.New("数据已更新，请刷新")

func seed() {
	e := db.Transaction(func(tx *gorm.DB) error {
		rows := []Exercise{{ID: 1, Name: "坐姿推胸", Muscle: "胸部", Equipment: "推胸机", Guide: "背部贴垫，向前推起后缓慢还原。", Setup: "调节座椅使把手与胸部同高。"}, {ID: 2, Name: "坐姿划船", Muscle: "背部", Equipment: "划船机", Guide: "肩部放松，将把手拉向腹部后还原。", Setup: "脚踩稳踏板，选择可控制的重量。"}, {ID: 3, Name: "腿举", Muscle: "腿部", Equipment: "腿举机", Guide: "保持背部贴垫，屈膝后平稳蹬起。", Setup: "确认安全限位，双脚平放踏板。"}}
		rows = append(rows,
			Exercise{ID: 4, Name: "杯式深蹲", Muscle: "腿部", Equipment: "哑铃", Guide: "将哑铃抱在胸前，下蹲后站起。", Setup: "选择能平稳控制的哑铃，双脚站稳。"},
			Exercise{ID: 5, Name: "高位下拉", Muscle: "背部", Equipment: "高位下拉机", Guide: "将横杆拉向上胸，缓慢回到起点。", Setup: "调整压腿垫，脚掌平放。"},
			Exercise{ID: 6, Name: "器械推肩", Muscle: "肩部", Equipment: "推肩机", Guide: "沿器械轨迹向上推起，缓慢还原。", Setup: "调节座椅，使把手接近肩部高度。"},
			Exercise{ID: 7, Name: "坐姿腿弯举", Muscle: "腿部", Equipment: "腿弯举机", Guide: "屈膝下压滚垫，缓慢还原。", Setup: "调整靠背，使膝关节对齐器械转轴。"},
			Exercise{ID: 8, Name: "臀桥", Muscle: "臀部", Equipment: "训练垫", Guide: "屈膝仰卧，抬髋后缓慢放下。", Setup: "平铺训练垫，脚掌踩稳。"})
		if e := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error; e != nil {
			return e
		}
		day := func(w int, focus string, ids ...int) Day {
			d := Day{Weekday: w, Focus: focus}
			for _, id := range ids {
				d.Items = append(d.Items, Item{rows[id-1].Name, 3, 10, 60})
			}
			return d
		}
		for _, p := range []Plan{
			{ID: 1, Name: "全身入门", Days: []Day{day(2, "全身", 4, 1, 2), day(5, "全身", 3, 5, 6)}, Published: true, Revision: 1},
			{ID: 2, Name: "上肢／下肢／全身", Days: []Day{day(1, "上肢", 1, 2), day(3, "下肢", 3, 7), day(6, "全身", 4, 5, 1)}, Published: true, Revision: 1},
			{ID: 3, Name: "上下肢交替", Days: []Day{day(1, "上肢", 1, 2), day(2, "下肢", 3, 7), day(4, "上肢", 5, 6), day(6, "下肢", 4, 8)}, Published: true, Revision: 1},
		} {
			if e := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&p).Error; e != nil {
				return e
			}
		}
		return nil
	})
	if e != nil {
		log.Fatal(e)
	}
}
func valid(p Plan) bool {
	if strings.TrimSpace(p.Name) == "" || len(p.Days) == 0 || len(p.Days) > 7 {
		return false
	}
	seen := map[int]bool{}
	for _, d := range p.Days {
		if seen[d.Weekday] || d.Weekday < 1 || d.Weekday > 7 || d.Focus == "" || len(d.Items) == 0 || len(d.Items) > 30 {
			return false
		}
		seen[d.Weekday] = true
		for _, i := range d.Items {
			if i.Name == "" || i.Sets < 1 || i.Sets > 30 || i.Reps < 1 || i.Reps > 200 || (i.Rest != 30 && i.Rest != 60 && i.Rest != 90) {
				return false
			}
		}
	}
	return true
}
func locked(tx *gorm.DB, id uint) (User, error) {
	var u User
	e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&u, id).Error
	return u, e
}
func result(c *gin.Context, v any, e error) {
	if errors.Is(e, conflict) {
		fail(c, 409, e.Error())
		return
	}
	save(c, v, e)
}
func registerPlans(g *gin.RouterGroup) {
	g.GET("/templates", func(c *gin.Context) {
		var v []Plan
		e := db.Where("owner=0 AND published=true").Find(&v).Error
		save(c, v, e)
	})
	g.GET("/plans", func(c *gin.Context) {
		var v []Plan
		e := db.Where("owner=?", uid(c)).Order("id desc").Find(&v).Error
		save(c, v, e)
	})
	g.POST("/plans", func(c *gin.Context) {
		var in struct {
			Operation                  string
			Template, Revision, Active uint
			Custom                     Plan
		}
		if c.ShouldBindJSON(&in) != nil {
			fail(c, 400, "方案无效")
			return
		}
		p := Plan{Name: in.Custom.Name, Days: in.Custom.Days}
		if in.Template == 0 && !valid(p) {
			fail(c, 400, "请检查训练安排")
			return
		}
		e := db.Transaction(func(tx *gorm.DB) error {
			u, e := locked(tx, uid(c))
			if e != nil {
				return e
			}
			if len(in.Operation) > 72 || len(in.Operation) < 8 {
				return conflict
			}
			var previous Plan
			err := tx.Where("owner=? AND operation_id=?", u.ID, in.Operation).First(&previous).Error
			if err == nil {
				p = previous
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if u.Active != in.Active {
				return conflict
			}
			if in.Template != 0 {
				if e := tx.Where("owner=0 AND published=true AND revision=?", in.Revision).First(&p, in.Template).Error; e != nil {
					return e
				}
			}
			p.ID = 0
			p.OperationID = &in.Operation
			p.Owner = u.ID
			p.Published = false
			if e := tx.Create(&p).Error; e != nil {
				return e
			}
			return tx.Model(&u).Update("active", p.ID).Error
		})
		result(c, p, e)
	})
}
func registerTraining(g *gin.RouterGroup) {
	g.GET("/sessions/:id", func(c *gin.Context) {
		var s Session
		e := db.Where("owner=? AND id=?", uid(c), c.Param("id")).First(&s).Error
		save(c, s, e)
	})
	g.GET("/sessions", func(c *gin.Context) {
		var v []Session
		e := db.Where("owner=?", uid(c)).Order("id desc").Limit(100).Find(&v).Error
		save(c, v, e)
	})
	g.POST("/sessions", func(c *gin.Context) {
		var in struct {
			Plan    uint
			Weekday int
		}
		if c.ShouldBindJSON(&in) != nil {
			fail(c, 400, "请选择计划")
			return
		}
		var s Session
		e := db.Transaction(func(tx *gorm.DB) error {
			u, e := locked(tx, uid(c))
			if e != nil {
				return e
			}
			e = tx.Where("owner=? AND ended IS NULL", u.ID).First(&s).Error
			if e == nil {
				return nil
			}
			if !errors.Is(e, gorm.ErrRecordNotFound) {
				return e
			}
			var p Plan
			if e = tx.Where("owner=?", u.ID).First(&p, in.Plan).Error; e != nil {
				return e
			}
			today := int(time.Now().In(time.FixedZone("CST", 8*3600)).Weekday())
			best := 8
			for _, d := range p.Days {
				if in.Weekday != 0 && d.Weekday != in.Weekday {
					continue
				}
				gap := (d.Weekday - today + 7) % 7
				if gap < best {
					best = gap
					s.Day = d
				}
			}
			if best == 8 {
				return conflict
			}
			s.PlanID = p.ID
			s.Owner = u.ID
			s.Started = time.Now()
			s.Done = []int{}
			return tx.Create(&s).Error
		})
		result(c, s, e)
	})
	g.PUT("/sessions/:id", func(c *gin.Context) {
		var in struct {
			Index, Reps  int
			Finish, Skip bool
			Weight       *float64
		}
		if c.ShouldBindJSON(&in) != nil || in.Index < 0 || (in.Weight != nil && (*in.Weight < 0 || *in.Weight > 2000)) || (!in.Finish && !in.Skip && (in.Reps < 1 || in.Reps > 200)) {
			fail(c, 400, "记录无效")
			return
		}
		var s Session
		e := db.Transaction(func(tx *gorm.DB) error {
			if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("owner=?", uid(c)).First(&s, "id=?", c.Param("id")).Error; e != nil {
				return e
			}
			if in.Index < len(s.Done) && !in.Finish && ((in.Skip && s.Done[in.Index] == 0) || (!in.Skip && s.Done[in.Index] == in.Reps && sameWeight(s.Weights, in.Index, in.Weight))) {
				return nil
			}
			if in.Index != len(s.Done) {
				return conflict
			}
			if in.Finish {
				now := time.Now()
				if s.Ended == nil {
					s.Ended = &now
				}
			} else {
				total := 0
				for _, i := range s.Day.Items {
					total += i.Sets
				}
				if s.Ended != nil || len(s.Done) >= total {
					return conflict
				}
				for len(s.Weights) < len(s.Done) {
					s.Weights = append(s.Weights, nil)
				}
				if in.Skip {
					end := 0
					for _, item := range s.Day.Items {
						end += item.Sets
						if end > in.Index {
							break
						}
					}
					for len(s.Done) < end {
						s.Done = append(s.Done, 0)
						s.Weights = append(s.Weights, nil)
					}
				} else {
					s.Done = append(s.Done, in.Reps)
					s.Weights = append(s.Weights, in.Weight)
				}
			}
			return tx.Save(&s).Error
		})
		result(c, s, e)
	})
}
func registerAdmin(g *gin.RouterGroup) {
	templateEditor(g)
	g.POST("/logout", func(c *gin.Context) {
		if e := db.Delete(&StaffSession{}, "id=?", c.GetString("sid")).Error; e != nil {
			save(c, nil, e)
			return
		}
		c.SetCookie("gym_admin", "", -1, "/api/admin", "", true, true)
		c.JSON(200, true)
	})
	g.GET("/exercises", func(c *gin.Context) { var rows []Exercise; e := db.Find(&rows).Error; save(c, rows, e) })
	g.POST("/exercises", func(c *gin.Context) {
		var x Exercise
		if c.ShouldBindJSON(&x) != nil || x.Name == "" || x.Guide == "" || x.Muscle == "" || x.Equipment == "" || x.Setup == "" {
			fail(c, 400, "请填写完整资料")
			return
		}
		if x.ImageURL != "" && !strings.HasPrefix(x.ImageURL, "https://") {
			fail(c, 400, "教学图须使用 HTTPS 地址")
			return
		}
		if x.ID != 0 {
			var old Exercise
			if e := db.First(&old, x.ID).Error; e != nil {
				save(c, nil, e)
				return
			}
			if old.Name != x.Name {
				fail(c, 409, "已建动作名称用于关联训练模板，请新增动作")
				return
			}
		}
		save(c, x, db.Save(&x).Error)
	})
	g.GET("/members", func(c *gin.Context) {
		var rows []User
		q := db.Order("id desc").Limit(100)
		if id := c.Query("id"); id != "" {
			q = q.Where("id=?", id)
		}
		e := q.Find(&rows).Error
		save(c, rows, e)
	})
	g.POST("/members/:id/grants", func(c *gin.Context) {
		var in struct {
			Days     int
			Reason   string
			Previous time.Time
		}
		if c.ShouldBindJSON(&in) != nil || in.Days < 1 || in.Days > 365 || strings.TrimSpace(in.Reason) == "" {
			fail(c, 400, "请输入天数和原因")
			return
		}
		e := db.Transaction(func(tx *gorm.DB) error {
			var u User
			if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&u, "id=?", c.Param("id")).Error; e != nil {
				return e
			}
			if !u.Expires.Equal(in.Previous) {
				return conflict
			}
			base := u.Expires
			if base.Before(time.Now()) {
				base = time.Now()
			}
			if e := tx.Create(&Grant{UserID: u.ID, Days: in.Days, Reason: in.Reason, Operator: "admin"}).Error; e != nil {
				return e
			}
			return tx.Model(&u).Update("expires", base.AddDate(0, 0, in.Days)).Error
		})
		result(c, true, e)
	})
}

func sameWeight(weights []*float64, index int, w *float64) bool {
	var other *float64
	if index < len(weights) {
		other = weights[index]
	}
	if other == nil || w == nil {
		return other == w
	}
	return *other == *w
}
