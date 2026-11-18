package handler

import (
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strconv"
	"strings"
	"time"
)

var conflict = errors.New("数据已更新，请刷新")

// 档位（会员套餐）的两种业务拒绝：天数重复、参数不合法。
// 校验发生在事务里（要读当前行再合并参数），不能直接写响应，所以包成 error 交给 result 翻译。
var dupTier = errors.New("该天数已有档位，换一个天数")

type invalidArg string

func (e invalidArg) Error() string { return string(e) }

func invalid(msg string) error { return invalidArg(msg) }

// 计划保存份数上限：免费 1 份、会员 6 份。上限只拦「新建」，改已有计划不计数，
// 所以会员到期后手上超过 1 份也还能继续改和删，只是暂时不能再加新的。
const (
	FreePlanLimit   = 1
	MemberPlanLimit = 6
)

var (
	freeLimit   = errors.New("免费版只能保存 1 份计划，开通会员可保存 6 份")
	memberLimit = errors.New("会员最多保存 6 份计划，删掉一份再新建")
)

// planLimit 按会员状态算当前能保存的份数，读 /me 与新建计划都以它为准。
func planLimit(u User) int {
	if u.Expires.After(time.Now()) {
		return MemberPlanLimit
	}
	return FreePlanLimit
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
	// 计划份数到顶属于「说得清的业务拒绝」：403 + 直出文案，前端 report 会原样弹出来
	if errors.Is(e, freeLimit) || errors.Is(e, memberLimit) {
		fail(c, 403, e.Error())
		return
	}
	// 档位编辑：参数不合法是 400，天数撞车是 409
	var bad invalidArg
	if errors.As(e, &bad) {
		fail(c, 400, bad.Error())
		return
	}
	if errors.Is(e, dupTier) {
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
			// ID 非 0 表示「改已有计划」：原地更新那一条，不再另存一份
			ID     uint
			Custom Plan
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
			// 改已有计划：只覆盖名称和训练日，owner/operation_id 保持不动。
			// 更新本身幂等，不用像新建那样按 Operation 查重放记录（那条重放要在
			// Active 校验之前，否则重试会被误判成 409）。
			if in.ID != 0 {
				if u.Active != in.Active {
					return conflict
				}
				var cur Plan
				if e := tx.Where("owner=? AND id=?", u.ID, in.ID).First(&cur).Error; e != nil {
					if errors.Is(e, gorm.ErrRecordNotFound) {
						return conflict
					}
					return e
				}
				cur.Name, cur.Days, cur.Published = p.Name, p.Days, false
				if e := tx.Save(&cur).Error; e != nil {
					return e
				}
				p = cur
				return tx.Model(&u).Update("active", cur.ID).Error
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
			// 份数上限：必须放在 Operation 重放查询之后 —— 同一次提交重试时应当返回已建好的那份，
			// 而不是被自己的上限挡住。这里只数 owner=自己的计划，官方模板（owner=0）不计入。
			var count int64
			if e := tx.Model(&Plan{}).Where("owner=?", u.ID).Count(&count).Error; e != nil {
				return e
			}
			if limit := planLimit(u); count >= int64(limit) {
				if limit == FreePlanLimit {
					return freeLimit
				}
				return memberLimit
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
			// 模板的封面与介绍文案不跟着复制：用户拿到的是可改的计划，留着那份推广文案反而会误导
			p.Meta = nil
			if e := tx.Create(&p).Error; e != nil {
				return e
			}
			return tx.Model(&u).Update("active", p.ID).Error
		})
		result(c, p, e)
	})
	// 删整份计划（连同它的训练日一起）。删的是当前计划时，当前位顶到剩下最新的一份；
	// 一份都不剩就置 0，前端据此回到「还没有计划」的引导态。历史训练记录是当时的 Day
	// 快照，不跟着删。返回 Active 让客户端不必再查一次 /me。
	g.DELETE("/plans/:id", func(c *gin.Context) {
		id, _ := strconv.Atoi(c.Param("id"))
		active := uint(0)
		e := db.Transaction(func(tx *gorm.DB) error {
			u, e := locked(tx, uid(c))
			if e != nil {
				return e
			}
			var p Plan
			if e := tx.Where("owner=? AND id=?", u.ID, id).First(&p).Error; e != nil {
				return e
			}
			if e := tx.Delete(&Plan{}, p.ID).Error; e != nil {
				return e
			}
			active = u.Active
			if u.Active != p.ID {
				return nil
			}
			var next Plan
			err := tx.Where("owner=?", u.ID).Order("id desc").First(&next).Error
			if err == nil {
				active = next.ID
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if e := tx.Model(&u).Update("active", active).Error; e != nil {
				return e
			}
			return nil
		})
		if e != nil {
			save(c, nil, e)
			return
		}
		save(c, gin.H{"Active": active}, nil)
	})
}

// pickDay 从计划里挑一个训练日：weekday 为 0 时取今天或之后最近的一天。
func pickDay(p Plan, weekday int) (Day, bool) {
	today := int(time.Now().In(time.FixedZone("CST", 8*3600)).Weekday())
	best := 8
	var pick Day
	for _, d := range p.Days {
		if weekday != 0 && d.Weekday != weekday {
			continue
		}
		if gap := (d.Weekday - today + 7) % 7; gap < best {
			best = gap
			pick = d
		}
	}
	return pick, best != 8
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
	// 预览：只按计划算出这次要练哪一天，不落库，供训练页在没有本地进度时开新的一场
	g.GET("/sessions/preview", func(c *gin.Context) {
		plan, _ := strconv.Atoi(c.Query("plan"))
		weekday, _ := strconv.Atoi(c.Query("weekday"))
		var p Plan
		if e := db.Where("owner=?", uid(c)).First(&p, plan).Error; e != nil {
			save(c, nil, e)
			return
		}
		day, ok := pickDay(p, weekday)
		if !ok {
			fail(c, 404, "这个计划没有安排训练日")
			return
		}
		c.JSON(200, gin.H{"PlanID": p.ID, "Day": day})
	})
	// 提交一场训练：客户端把整段记录一次传上来，operation 保证重试不会写两条
	g.POST("/sessions", func(c *gin.Context) {
		var in struct {
			Plan      uint
			Weekday   int
			Started   time.Time
			Done      []int
			Weights   []*float64
			Operation string
		}
		if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.Operation) == "" || len(in.Done) == 0 {
			fail(c, 400, "记录无效")
			return
		}
		var p Plan
		if e := db.Where("owner=?", uid(c)).First(&p, in.Plan).Error; e != nil {
			save(c, nil, e)
			return
		}
		day, ok := pickDay(p, in.Weekday)
		if !ok {
			fail(c, 400, "这个计划没有安排训练日")
			return
		}
		total := 0
		for _, i := range day.Items {
			total += i.Sets
		}
		if len(in.Done) != total {
			fail(c, 400, "组数和计划不一致")
			return
		}
		for _, n := range in.Done {
			if n < 0 || n > 200 {
				fail(c, 400, "次数须为 1 至 200")
				return
			}
		}
		for _, w := range in.Weights {
			if w != nil && (*w < 0 || *w > 2000) {
				fail(c, 400, "重量无效")
				return
			}
		}
		for len(in.Weights) < len(in.Done) {
			in.Weights = append(in.Weights, nil)
		}
		in.Weights = in.Weights[:len(in.Done)]
		started := in.Started
		if started.IsZero() || started.After(time.Now()) {
			started = time.Now()
		}
		ended := time.Now()
		s := Session{Owner: uid(c), PlanID: p.ID, Day: day, Done: in.Done, Weights: in.Weights, Started: started, Ended: &ended, OperationID: &in.Operation}
		if e := db.Create(&s).Error; e != nil {
			if !strings.Contains(e.Error(), "Duplicate entry") {
				save(c, nil, e)
				return
			}
			if e := db.Where("owner=? AND operation_id=?", uid(c), in.Operation).First(&s).Error; e != nil {
				save(c, nil, e)
				return
			}
		}
		c.JSON(200, s)
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
	g.GET("/exercises", exerciseList)
	// 部位与器材分类的约定顺序：后台下拉和小程序侧边栏共用一份，避免两边手工对齐。
	g.GET("/categories", categories)
	// 用户反馈：后台按时间倒序看最新 200 条，可按状态、用户编号或内容关键词筛。
	// 用户报上来的是编号（U3M8Q2K），后台旧习惯的纯数字 ID 也照旧能搜。
	g.GET("/feedbacks", func(c *gin.Context) {
		var rows []Feedback
		q := db.Order("id desc").Limit(200)
		if st := strings.TrimSpace(c.Query("status")); st != "" {
			q = q.Where("status=?", st)
		}
		if owner := strings.TrimSpace(c.Query("owner")); owner != "" {
			id, ok := ownerID(owner)
			if !ok {
				// 编号查不到人就当作没这个人：返回空列表，比 400 更贴近「没搜到」
				save(c, []Feedback{}, nil)
				return
			}
			q = q.Where("owner=?", id)
		}
		if kw := strings.TrimSpace(c.Query("q")); kw != "" {
			q = q.Where("content LIKE ?", "%"+kw+"%")
		}
		e := q.Find(&rows).Error
		owners := make([]uint, 0, len(rows))
		for _, r := range rows {
			owners = append(owners, r.Owner)
		}
		codes := codesOf(owners)
		for i := range rows {
			rows[i].OwnerCode = codes[rows[i].Owner]
		}
		save(c, rows, e)
	})
	// 标记处理状态：已处理时记处理人与时间，退回待处理时清空
	g.PUT("/feedbacks/:id", func(c *gin.Context) {
		var in struct {
			Status string
			Note   string
		}
		if c.ShouldBindJSON(&in) != nil || (in.Status != "待处理" && in.Status != "已处理") {
			fail(c, 400, "处理状态无效")
			return
		}
		if len([]rune(in.Note)) > 500 {
			fail(c, 400, "备注最多 500 字")
			return
		}
		var row Feedback
		if e := db.First(&row, "id=?", c.Param("id")).Error; e != nil {
			save(c, nil, e)
			return
		}
		updates := map[string]any{"status": in.Status, "note": strings.TrimSpace(in.Note)}
		if in.Status == "已处理" {
			updates["handled_by"] = c.GetString("actor")
			updates["handled_at"] = time.Now()
		} else {
			updates["handled_by"] = nil
			updates["handled_at"] = nil
		}
		if e := db.Model(&Feedback{}).Where("id=?", row.ID).Updates(updates).Error; e != nil {
			save(c, nil, e)
			return
		}
		// 用新结构体重新读，避免在已有值上扫描 NULL 时保留旧的指针字段
		var fresh Feedback
		save(c, fresh, db.First(&fresh, row.ID).Error)
	})
	g.POST("/exercises", func(c *gin.Context) {
		var x Exercise
		if c.ShouldBindJSON(&x) != nil || x.Name == "" || x.Guide == "" || x.Equipment == "" || x.Setup == "" {
			fail(c, 400, "请填写完整资料")
			return
		}
		if msg := taxonomy(x); msg != "" {
			fail(c, 400, msg)
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
	// 会员列表：支持按用户编号（U3M8Q2K）或纯数字 ID 查人，客服照着用户报的编号就能找到
	g.GET("/members", func(c *gin.Context) {
		var rows []User
		q := db.Order("id desc").Limit(100)
		if owner := strings.TrimSpace(c.Query("id")); owner != "" {
			id, ok := ownerID(owner)
			if !ok {
				save(c, []User{}, nil)
				return
			}
			q = q.Where("id=?", id)
		}
		save(c, rows, q.Find(&rows).Error)
	})
	g.POST("/members/:id/grants", func(c *gin.Context) {
		if !c.GetBool("admin") {
			fail(c, 403, "需要管理员权限")
			return
		}
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
	// 注销账号：把这个人彻底删掉 —— 计划、训练记录、反馈、人工权益流水和账号行一起删。
	// 已支付/已退款的订单不删：交易要留档，但把 owner 置 0 脱敏，从此对不上具体的人；
	// 退款回调也是靠 owner 找用户，所以 refund 里对 owner=0 单独处理。
	// 还有「待支付」订单时直接拒绝：那一刻钱可能刚付出去、回调还没到，删了就对不上账。
	g.DELETE("/members/:id", func(c *gin.Context) {
		if !c.GetBool("admin") {
			fail(c, 403, "需要管理员权限")
			return
		}
		id, e := strconv.ParseUint(c.Param("id"), 10, 32)
		if e != nil {
			fail(c, 400, "账号无效")
			return
		}
		// 要求照着用户编号再输一遍：注销不可恢复，只靠点一次确认太容易误操作
		var in struct{ Code string }
		if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.Code) == "" {
			fail(c, 400, "请输入用户编号确认")
			return
		}
		var count struct{ Plans, Sessions, Feedbacks, Grants, Orders int64 }
		e = db.Transaction(func(tx *gorm.DB) error {
			var u User
			if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&u, "id=?", id).Error; e != nil {
				return e
			}
			if !strings.EqualFold(strings.TrimSpace(in.Code), u.Code) {
				return invalid("用户编号不匹配，未执行注销")
			}
			var pending int64
			if e := tx.Model(&Order{}).Where("owner=? AND status=?", u.ID, "待支付").Count(&pending).Error; e != nil {
				return e
			}
			if pending > 0 {
				return invalid("该账号还有未完成的订单，等支付结果确认后再注销")
			}
			plans := tx.Where("owner=?", u.ID).Delete(&Plan{})
			if plans.Error != nil {
				return plans.Error
			}
			sessions := tx.Where("owner=?", u.ID).Delete(&Session{})
			if sessions.Error != nil {
				return sessions.Error
			}
			feedbacks := tx.Where("owner=?", u.ID).Delete(&Feedback{})
			if feedbacks.Error != nil {
				return feedbacks.Error
			}
			grants := tx.Where("user_id=?", u.ID).Delete(&Grant{})
			if grants.Error != nil {
				return grants.Error
			}
			count.Plans, count.Sessions = plans.RowsAffected, sessions.RowsAffected
			count.Feedbacks, count.Grants = feedbacks.RowsAffected, grants.RowsAffected
			// 订单脱敏：owner 置 0，其余字段（金额、天数、支付状态、渠道单号）留着对账
			res := tx.Model(&Order{}).Where("owner=?", u.ID).Update("owner", 0)
			if res.Error != nil {
				return res.Error
			}
			count.Orders = res.RowsAffected
			if e := tx.Delete(&User{}, u.ID).Error; e != nil {
				return e
			}
			action := fmt.Sprintf("注销账号 %s：计划 %d、训练记录 %d、反馈 %d、权益流水 %d、订单 %d 笔脱敏保留",
				u.Code, count.Plans, count.Sessions, count.Feedbacks, count.Grants, count.Orders)
			return tx.Create(&Audit{Actor: c.GetString("actor"), Action: action, Target: u.ID}).Error
		})
		result(c, count, e)
	})
}
