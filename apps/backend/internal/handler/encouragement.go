package handler

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Encouragement 是首页弹窗里的一句鼓励，由后台维护。
//
// 一天只推一条，用两列把「本轮」和「历史」分开：
//   - served_on 管本轮：NULL=未推送、=今天=今日推送、早于今天=已推送。整轮推完后会全部清回 NULL，开始新一轮。
//   - last_served 管历史：最后一次被推送的日期，跨轮保留，后台用它显示「上次推送」，抽取时用它避开刚推过的那条。
//
// 状态不落库，读出来时按 served_on 和今天的关系现算，所以第二天不需要定时任务去「把昨天的标成已推送」。
type Encouragement struct {
	ID      uint
	Text    string
	Author  string
	Enabled bool
	Sort    int
	// ServedOn / LastServed 落库是 DATE；JSON 里换成下面两个现算字段，省得前端自己算状态
	ServedOn   *time.Time `gorm:"type:date" json:"-"`
	LastServed *time.Time `gorm:"type:date" json:"-"`
	Status     string     `gorm:"-" json:"Status"`
	Served     string     `gorm:"-" json:"Served"`
	CreatedAt  time.Time
}

// encouragementCard 是小程序侧拿到的字段：只有一句文案，后台的启用/排序/时间不下发。
type encouragementCard struct {
	ID     uint   `json:"ID"`
	Text   string `json:"Text"`
	Author string `json:"Author"`
}

// 文案上限：80 个汉字在弹窗里三行内能排下，再长就失去「一眼看完」的作用。
const (
	MaxEncouragementRunes = 80
	MaxEncouragements     = 200
)

var (
	// 取今日推送要抢锁，抢不到说明另一个请求正在抽，让客户端下次再来
	errEncourageBusy = errors.New("正在生成今天的文案，请稍后再试")
	// 抽签用的北京时间：和 plans 的「今天」、前端的展示口径保持一致
	cstZone = time.FixedZone("CST", 8*3600)
)

func todayCST() string { return time.Now().In(cstZone).Format("2006-01-02") }

// fillStatus 把两个日期翻成后台看得懂的三态 + 上次推送日期。
func fillStatus(e *Encouragement, today string) {
	e.Status, e.Served = "未推送", ""
	if e.ServedOn != nil {
		e.Status = "已推送"
		if e.ServedOn.Format("2006-01-02") == today {
			e.Status = "今日推送"
		}
	}
	// 未推送的行也可能有「上次推送」——那是上一轮推的
	if e.LastServed != nil {
		e.Served = e.LastServed.Format("2006-01-02")
	}
}

// todaysCard 返回今天这一条：当天第一次被请求时抽一条「未推送」的落库，之后所有人拿到同一条。
// 未推送抽完了就把本轮清空、开新一轮（见下面那个分支），所以「轮次」永远有进度可看。
//
// 抽签用 MySQL 的 GET_LOCK 串行化——两个用户同时进首页也只会产生一条今日推送；
// 锁必须和后续写操作在同一条连接上，所以整段包在 db.Connection 里（同 migrations.Run 的做法）。
// 表上另有 served_on 的唯一索引兜底：就算多实例抢锁失败，也写不进第二条。
func todaysCard() (encouragementCard, error) {
	var card encouragementCard
	today := todayCST()
	e := db.Connection(func(conn *gorm.DB) error {
		// 每次查询都用独立会话，避免 Where 条件被链式调用累加
		fresh := func() *gorm.DB { return conn.Session(&gorm.Session{}) }
		var got int
		if e := conn.Raw("SELECT GET_LOCK('gym_encourage_today', 5)").Scan(&got).Error; e != nil {
			return e
		}
		if got != 1 {
			return errEncourageBusy
		}
		defer conn.Exec("SELECT RELEASE_LOCK('gym_encourage_today')")

		var cur Encouragement
		if e := fresh().Where("served_on = ?", today).Limit(1).Find(&cur).Error; e != nil {
			return e
		}
		if cur.ID != 0 {
			if cur.Enabled {
				card = cardOf(cur)
				return nil
			}
			// 今天这条被停用了：先让出今日位，再重新抽一条
			if e := fresh().Model(&Encouragement{}).Where("id = ?", cur.ID).Update("served_on", nil).Error; e != nil {
				return e
			}
		}

		// 未推送池里随机取一条
		var pick Encouragement
		if e := fresh().Where("enabled = ? AND served_on IS NULL", true).Order("RAND()").Limit(1).Find(&pick).Error; e != nil {
			return e
		}
		if pick.ID == 0 {
			// 整轮都推完了：把本轮清回「未推送」再开始新一轮。
			// 不清的话后台永远看不到「未推送」，日期也会跨轮混在一起，分不清哪条是本轮推的。
			// 历史不丢——最后一次推送时间在 last_served 里，这里清的是本轮位。
			if e := fresh().Model(&Encouragement{}).Where("served_on IS NOT NULL").Update("served_on", nil).Error; e != nil {
				return e
			}
			var last Encouragement
			if e := fresh().Where("enabled = ? AND last_served IS NOT NULL", true).Order("last_served desc").Limit(1).Find(&last).Error; e != nil {
				return e
			}
			q := fresh().Where("enabled = ?", true)
			// 避开昨天刚推过的那条，否则「最后一条」紧接着又成了「下一条」，看着像没换
			if last.ID != 0 {
				q = q.Where("id <> ?", last.ID)
			}
			if e := q.Order("RAND()").Limit(1).Find(&pick).Error; e != nil {
				return e
			}
			if pick.ID == 0 && last.ID != 0 {
				// 可推的只剩这一条：还是它，总得有人说话
				pick = last
			}
			if pick.ID == 0 {
				// 一条可用文案都没有：不下发，客户端这次就不弹
				return nil
			}
		}
		if e := fresh().Model(&Encouragement{}).Where("id = ?", pick.ID).Updates(map[string]any{
			"served_on": today, "last_served": today,
		}).Error; e != nil {
			return e
		}
		card = cardOf(pick)
		return nil
	})
	if e != nil {
		return encouragementCard{}, e
	}
	return card, nil
}

func cardOf(e Encouragement) encouragementCard {
	return encouragementCard{ID: e.ID, Text: e.Text, Author: e.Author}
}

// pushNow 把指定的一句手动设为今日推送，和当天的自动抽签抢同一把锁。
//
// 原本今天那条退回「未推送」而不是「已推送」：运营手动换一条，通常是想马上换掉今天的内容，
// 被换下的那句还值得在后面某天再推，不该因为被顶替就整轮报废。
func pushNow(c *gin.Context) {
	if !c.GetBool("admin") {
		fail(c, 403, "需要管理员权限")
		return
	}
	id, e := strconv.ParseUint(c.Param("id"), 10, 32)
	if e != nil {
		fail(c, 400, "文案无效")
		return
	}
	today := todayCST()
	var row Encouragement
	e = db.Connection(func(conn *gorm.DB) error {
		fresh := func() *gorm.DB { return conn.Session(&gorm.Session{}) }
		var got int
		if e := conn.Raw("SELECT GET_LOCK('gym_encourage_today', 5)").Scan(&got).Error; e != nil {
			return e
		}
		if got != 1 {
			return errEncourageBusy
		}
		defer conn.Exec("SELECT RELEASE_LOCK('gym_encourage_today')")
		return conn.Transaction(func(tx *gorm.DB) error {
			if e := tx.First(&row, "id = ?", id).Error; e != nil {
				return e
			}
			if !row.Enabled {
				return invalid("先启用这句，再设为今日推送")
			}
			// 已经是今天这条：什么都不用改，也不写审计
			if row.ServedOn != nil && row.ServedOn.Format("2006-01-02") == today {
				return nil
			}
			// 先让出今日位，再占上去。顺序不能反：served_on 上有唯一索引
			var old Encouragement
			if e := fresh().Where("served_on = ? AND id <> ?", today, row.ID).Limit(1).Find(&old).Error; e != nil {
				return e
			}
			if old.ID != 0 {
				if e := tx.Model(&Encouragement{}).Where("id = ?", old.ID).Update("served_on", nil).Error; e != nil {
					return e
				}
			}
			if e := tx.Model(&Encouragement{}).Where("id = ?", row.ID).Updates(map[string]any{
				"served_on": today, "last_served": today,
			}).Error; e != nil {
				return e
			}
			action := "指定今日推送「" + row.Text + "」"
			if old.ID != 0 {
				action += "，原「" + old.Text + "」退回未推送"
			}
			return tx.Create(&Audit{Actor: c.GetString("actor"), Action: action, Target: row.ID}).Error
		})
	})
	if e != nil {
		if errors.Is(e, errEncourageBusy) {
			fail(c, 503, e.Error())
			return
		}
		result(c, nil, e)
		return
	}
	// 事务里只写了两列，回读一次拿到最新的 served_on/last_served，再把状态算出来
	if e := db.First(&row, "id = ?", id).Error; e != nil {
		save(c, nil, e)
		return
	}
	fillStatus(&row, todayCST())
	c.JSON(200, row)
}

// checkEncouragement 校验文案与落款，返回空串表示通过。
func checkEncouragement(text, author string) string {
	n := len([]rune(text))
	if n == 0 {
		return "请填写鼓励语"
	}
	if n > MaxEncouragementRunes {
		return fmt.Sprintf("鼓励语最多 %d 个字", MaxEncouragementRunes)
	}
	if len([]rune(author)) > 16 {
		return "落款最多 16 个字"
	}
	return ""
}

// encouragementRoutes 同时挂两套路由：
//   - g 是小程序侧（公开接口，只回今天这一条）；
//   - a 是后台侧（增删改查，需管理员）。
func encouragementRoutes(g, a *gin.RouterGroup) {
	// 小程序：只要今天这一条。没配文案（或全停用）时返回 null，客户端据此不弹。
	g.GET("/encouragements/today", func(c *gin.Context) {
		card, e := todaysCard()
		if errors.Is(e, errEncourageBusy) {
			fail(c, 503, e.Error())
			return
		}
		if e != nil {
			save(c, nil, e)
			return
		}
		if card.ID == 0 {
			c.JSON(200, nil)
			return
		}
		c.JSON(200, card)
	})

	// 后台：全部文案（含停用），带三态状态与推送日期
	a.GET("/encouragements", func(c *gin.Context) {
		var v []Encouragement
		if e := db.Order("`sort`, id").Find(&v).Error; e != nil {
			save(c, nil, e)
			return
		}
		today := todayCST()
		for i := range v {
			fillStatus(&v[i], today)
		}
		save(c, v, nil)
	})
	a.POST("/encouragements", func(c *gin.Context) {
		if !c.GetBool("admin") {
			fail(c, 403, "需要管理员权限")
			return
		}
		var in struct {
			Text, Author string
			Enabled      *bool
			Sort         *int
		}
		if c.ShouldBindJSON(&in) != nil {
			fail(c, 400, "参数无效")
			return
		}
		in.Text, in.Author = strings.TrimSpace(in.Text), strings.TrimSpace(in.Author)
		if msg := checkEncouragement(in.Text, in.Author); msg != "" {
			fail(c, 400, msg)
			return
		}
		enabled := true
		if in.Enabled != nil {
			enabled = *in.Enabled
		}
		// 新文案一定从「未推送」开始：进池子排队，不能一建出来就抢今天的位子
		row := Encouragement{Text: in.Text, Author: in.Author, Enabled: enabled, Sort: 10, CreatedAt: time.Now()}
		e := db.Transaction(func(tx *gorm.DB) error {
			var count int64
			if e := tx.Model(&Encouragement{}).Count(&count).Error; e != nil {
				return e
			}
			if count >= MaxEncouragements {
				return invalid(fmt.Sprintf("最多保存 %d 条文案，先删掉一些", MaxEncouragements))
			}
			// 没指定排序就接到末尾：新加的文案不该插到最前面把老文案挤下去
			if in.Sort != nil {
				row.Sort = *in.Sort
			} else {
				var last Encouragement
				if e := tx.Order("`sort` desc").First(&last).Error; e == nil {
					row.Sort = last.Sort + 10
				}
			}
			if e := tx.Create(&row).Error; e != nil {
				return e
			}
			return tx.Create(&Audit{Actor: c.GetString("actor"), Action: "新增鼓励语「" + row.Text + "」", Target: row.ID}).Error
		})
		if e == nil {
			// 新建的一定是「未推送」，补上让后台列表不用自己推导
			fillStatus(&row, todayCST())
		}
		result(c, row, e)
	})
	a.PUT("/encouragements/:id", func(c *gin.Context) {
		if !c.GetBool("admin") {
			fail(c, 403, "需要管理员权限")
			return
		}
		// 全部用指针：只想改上下架时传 Enabled 一个字段就够
		var in struct {
			Text, Author *string
			Enabled      *bool
			Sort         *int
			// Reset 把这句放回「未推送」：今天这条写错了要换掉、或想让某句重新排队时用
			Reset bool
		}
		if c.ShouldBindJSON(&in) != nil {
			fail(c, 400, "参数无效")
			return
		}
		var row Encouragement
		e := db.Transaction(func(tx *gorm.DB) error {
			if e := tx.First(&row, "id = ?", c.Param("id")).Error; e != nil {
				return e
			}
			next, changes := row, []string{}
			if in.Text != nil {
				next.Text = strings.TrimSpace(*in.Text)
				if next.Text != row.Text {
					changes = append(changes, "文案「"+row.Text+"」→「"+next.Text+"」")
				}
			}
			if in.Author != nil {
				next.Author = strings.TrimSpace(*in.Author)
				if next.Author != row.Author {
					changes = append(changes, "落款「"+show(row.Author, "未填写")+"」→「"+show(next.Author, "未填写")+"」")
				}
			}
			if in.Sort != nil {
				next.Sort = *in.Sort
				if next.Sort != row.Sort {
					changes = append(changes, "排序 "+strconv.Itoa(row.Sort)+"→"+strconv.Itoa(next.Sort))
				}
			}
			if in.Enabled != nil && *in.Enabled != row.Enabled {
				next.Enabled = *in.Enabled
				if next.Enabled {
					changes = append(changes, "启用")
				} else {
					// 停用即刻退出今日推送位，下一个人进来就会换一条
					next.ServedOn = nil
					changes = append(changes, "停用")
				}
			}
			if in.Reset && next.ServedOn != nil {
				next.ServedOn = nil
				changes = append(changes, "放回未推送")
			}
			if msg := checkEncouragement(next.Text, next.Author); msg != "" {
				return invalid(msg)
			}
			if e := tx.Model(&Encouragement{}).Where("id = ?", row.ID).Updates(map[string]any{
				"text": next.Text, "author": next.Author, "enabled": next.Enabled,
				"sort": next.Sort, "served_on": next.ServedOn,
			}).Error; e != nil {
				return e
			}
			row = next
			// 什么都没改的 PUT 不写审计：连点保存不该多出一条没有信息的记录
			if len(changes) == 0 {
				return nil
			}
			action := "修改鼓励语「" + next.Text + "」：" + strings.Join(changes, "，")
			return tx.Create(&Audit{Actor: c.GetString("actor"), Action: action, Target: row.ID}).Error
		})
		if e == nil {
			// 事务里只动了自己的字段，状态要按今天重新算一遍再下发
			fillStatus(&row, todayCST())
		}
		result(c, row, e)
	})
	// 手动指定今天推哪条（新建完想立刻用上、或今天这条不合适时用）
	a.POST("/encouragements/:id/push", pushNow)
	a.DELETE("/encouragements/:id", func(c *gin.Context) {
		if !c.GetBool("admin") {
			fail(c, 403, "需要管理员权限")
			return
		}
		e := db.Transaction(func(tx *gorm.DB) error {
			var row Encouragement
			if e := tx.First(&row, "id = ?", c.Param("id")).Error; e != nil {
				return e
			}
			if e := tx.Delete(&Encouragement{}, row.ID).Error; e != nil {
				return e
			}
			return tx.Create(&Audit{Actor: c.GetString("actor"), Action: "删除鼓励语「" + row.Text + "」", Target: row.ID}).Error
		})
		save(c, true, e)
	})
}
