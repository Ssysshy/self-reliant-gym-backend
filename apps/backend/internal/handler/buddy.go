package handler

import (
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Buddy 是首页右下角的「健身搭子」：一张人物图加几句台词，由后台维护。
//
// 用户可以从列表里挑一个收藏（users.buddy_id），也可以不挑——不挑就展示第一个。
// 挑中的存在两边：个人信息（换手机登录还能找回来）+ 小程序本地（进首页不用等接口）。
//
// 图片和动作示范图一个口径：只存相对路径（/assets/... 或 CDN 上的路径），
// 域名由前端拼 CDN 前缀，换 CDN 不用改数据。
type Buddy struct {
	ID       uint
	Name     string
	ImageURL string
	// Quotes 是点一下搭子随机冒出来的那几句，存 JSON 数组
	Quotes    []string `gorm:"serializer:json;type:json"`
	Enabled   bool
	Sort      int
	CreatedAt time.Time
}

func (Buddy) TableName() string { return "buddies" }

// 上限：搭子是首页的装饰，列表太长反而挑不动；单句限 60 字是因为气泡只有两行宽。
const (
	MaxBuddies     = 50
	MaxBuddyQuotes = 20
	MaxQuoteRunes  = 60
)

// cleanBuddy 把后台表单里的脏数据收拾干净：去掉首尾空白、丢掉空行、同名台词去重。
func cleanBuddy(name, image string, quotes []string) (string, string, []string) {
	name, image = strings.TrimSpace(name), strings.TrimSpace(image)
	out := make([]string, 0, len(quotes))
	seen := map[string]bool{}
	for _, q := range quotes {
		q = strings.TrimSpace(q)
		if q == "" || seen[q] {
			continue
		}
		seen[q] = true
		out = append(out, q)
	}
	return name, image, out
}

// checkBuddy 校验一条搭子。图片走 imagePath()：相对路径或 https 都行，http 拦掉。
func checkBuddy(name, image string, quotes []string) string {
	if len([]rune(name)) > 16 {
		return "名字最多 16 个字"
	}
	if image == "" {
		return "请填写人物图片地址"
	}
	if !imagePath(image) {
		return "图片请填相对路径或 https 地址"
	}
	if len(quotes) == 0 {
		return "至少写一句话"
	}
	if len(quotes) > MaxBuddyQuotes {
		return "最多 20 句，够点很多次了"
	}
	for _, q := range quotes {
		if len([]rune(q)) > MaxQuoteRunes {
			return "单句最多 60 个字"
		}
	}
	return ""
}

// buddyRoutes 挂三组路由：
//   - api：小程序公开列表（只要启用的）；
//   - u：用户选择搭子，写进个人信息；
//   - a：后台增删改查，需管理员。
func buddyRoutes(api, u, a *gin.RouterGroup) {
	// 小程序：整份列表一起下发。台词也跟着走——点搭子要的是「随机一句」，
	// 为这个再跑一趟接口不值当，列表本身也就几十条。
	api.GET("/buddies", func(c *gin.Context) {
		v := []Buddy{}
		save(c, v, db.Where("enabled = ?", true).Order("`sort`, id").Find(&v).Error)
	})

	// 用户：把挑中的搭子存进个人信息。
	// 不校验「必须已选过」——直接覆盖，换一个就是再调一次。
	u.PUT("/me/buddy", func(c *gin.Context) {
		var in struct{ ID uint }
		if c.ShouldBindJSON(&in) != nil || in.ID == 0 {
			fail(c, 400, "请选择一个健身搭子")
			return
		}
		var b Buddy
		if e := db.Where("id = ? AND enabled = ?", in.ID, true).First(&b).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				fail(c, 404, "这个搭子已经下架了，换一个吧")
				return
			}
			save(c, nil, e)
			return
		}
		save(c, b, db.Model(&User{}).Where("id = ?", uid(c)).Update("buddy_id", b.ID).Error)
	})

	// 后台：全部搭子（含停用），顺序与小程序一致
	a.GET("/buddies", func(c *gin.Context) {
		v := []Buddy{}
		save(c, v, db.Order("`sort`, id").Find(&v).Error)
	})
	a.POST("/buddies", func(c *gin.Context) {
		if !c.GetBool("admin") {
			fail(c, 403, "需要管理员权限")
			return
		}
		var in struct {
			Name, ImageURL string
			Quotes         []string
			Enabled        *bool
			Sort           *int
		}
		if c.ShouldBindJSON(&in) != nil {
			fail(c, 400, "参数无效")
			return
		}
		name, image, quotes := cleanBuddy(in.Name, in.ImageURL, in.Quotes)
		if msg := checkBuddy(name, image, quotes); msg != "" {
			fail(c, 400, msg)
			return
		}
		row := Buddy{Name: name, ImageURL: image, Quotes: quotes, Enabled: true, Sort: 10, CreatedAt: time.Now()}
		if in.Enabled != nil {
			row.Enabled = *in.Enabled
		}
		e := db.Transaction(func(tx *gorm.DB) error {
			var count int64
			if e := tx.Model(&Buddy{}).Count(&count).Error; e != nil {
				return e
			}
			if count >= MaxBuddies {
				return invalid("最多 50 个搭子，先删掉一些")
			}
			// 没指定排序就接在末尾，新加的不会插到最前面把老搭子挤下去
			if in.Sort != nil {
				row.Sort = *in.Sort
			} else {
				var last Buddy
				if e := tx.Order("`sort` desc").First(&last).Error; e == nil {
					row.Sort = last.Sort + 10
				}
			}
			if e := tx.Create(&row).Error; e != nil {
				return e
			}
			return tx.Create(&Audit{Actor: c.GetString("actor"), Action: "新增健身搭子「" + row.Name + "」", Target: row.ID}).Error
		})
		result(c, row, e)
	})
	a.PUT("/buddies/:id", func(c *gin.Context) {
		if !c.GetBool("admin") {
			fail(c, 403, "需要管理员权限")
			return
		}
		var in struct {
			Name, ImageURL string
			Quotes         []string
			Enabled        *bool
			Sort           *int
		}
		if c.ShouldBindJSON(&in) != nil {
			fail(c, 400, "参数无效")
			return
		}
		name, image, quotes := cleanBuddy(in.Name, in.ImageURL, in.Quotes)
		if msg := checkBuddy(name, image, quotes); msg != "" {
			fail(c, 400, msg)
			return
		}
		var row Buddy
		e := db.Transaction(func(tx *gorm.DB) error {
			if e := tx.First(&row, "id = ?", c.Param("id")).Error; e != nil {
				return e
			}
			row.Name, row.ImageURL, row.Quotes = name, image, quotes
			if in.Enabled != nil {
				row.Enabled = *in.Enabled
			}
			if in.Sort != nil {
				row.Sort = *in.Sort
			}
			if e := tx.Save(&row).Error; e != nil {
				return e
			}
			return tx.Create(&Audit{Actor: c.GetString("actor"), Action: "修改健身搭子「" + row.Name + "」", Target: row.ID}).Error
		})
		result(c, row, e)
	})
	a.DELETE("/buddies/:id", func(c *gin.Context) {
		if !c.GetBool("admin") {
			fail(c, 403, "需要管理员权限")
			return
		}
		var row Buddy
		e := db.Transaction(func(tx *gorm.DB) error {
			if e := tx.First(&row, "id = ?", c.Param("id")).Error; e != nil {
				return e
			}
			// 收藏过它的人清回 0：小程序会退回展示列表第一个，
			// 留着悬空的 id 虽然也能兜底，但下次拉 /me 会一直带一个不存在的值
			if e := tx.Model(&User{}).Where("buddy_id = ?", row.ID).Update("buddy_id", 0).Error; e != nil {
				return e
			}
			if e := tx.Delete(&row).Error; e != nil {
				return e
			}
			return tx.Create(&Audit{Actor: c.GetString("actor"), Action: "删除健身搭子「" + row.Name + "」", Target: row.ID}).Error
		})
		save(c, true, e)
	})
}
