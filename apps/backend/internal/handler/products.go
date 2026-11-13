package handler

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Product 是一个会员档位。days 既是主键也是 orders.days 的取值，所以档位之间天数不能重复；
// 改天数等于改主键，见 PUT 分支里那句显式 Updates。
type Product struct {
	Days    int `gorm:"primaryKey;autoIncrement:false"`
	Price   int
	Enabled bool
	// Note 是套餐卡上那行说明，留空时小程序回落到「会员有效期 N 天」
	Note string
	// XpayID 是微信虚拟支付的商品 ID（落库列 product）；留空时回落到环境变量 XPAY_PRODUCT_<days>
	XpayID string `gorm:"column:product" json:"XpayID"`
	// Xpay 与 Ready 不落库，下发前现算：实际生效的商品 ID、支付配置是否齐全
	Xpay  string `gorm:"-" json:"Xpay"`
	Ready bool   `gorm:"-" json:"Ready"`
}

// xpayID 返回档位实际生效的微信商品 ID：后台填了就用库里的，
// 没填回落到 XPAY_PRODUCT_<days>，存量三档不改配置也能继续卖。
func xpayID(p Product) string {
	if p.XpayID != "" {
		return p.XpayID
	}
	return os.Getenv("XPAY_PRODUCT_" + strconv.Itoa(p.Days))
}

// ready 判断这一档现在能不能真的下单：商品 ID、售价、密钥、推送配置缺一个都会在 purchase 里 503。
func ready(p Product) bool {
	return p.Price > 0 && xpayID(p) != "" &&
		os.Getenv("XPAY_OFFER") != "" && os.Getenv("XPAY_KEY") != "" &&
		os.Getenv("WX_PUSH_TOKEN") != "" && os.Getenv("WX_PUSH_AES") != ""
}

// fill 下发前把算出来的字段填好，前端不用自己拼环境变量。
func fill(p Product) Product {
	p.Xpay = xpayID(p)
	p.Ready = ready(p)
	return p
}

// show 把空值换成好读的占位：审计里「未配置」比两个引号中间什么都没有要清楚。
func show(v, empty string) string {
	if v == "" {
		return empty
	}
	return v
}

// checkProduct 校验档位参数，返回空串表示通过。
// 天数既决定 orders.days 也决定权益时长，限在 1~3650 天，避免建出 0 天卡或十年卡。
func checkProduct(days, price int, note, xpay string) string {
	if days < 1 || days > 3650 {
		return "有效天数需在 1~3650 天之间"
	}
	if price < 0 || price > 10000000 {
		return "售价需在 0~100000 元之间"
	}
	if len([]rune(note)) > 32 {
		return "说明最多 32 个字"
	}
	if len(xpay) > 128 {
		return "商品 ID 过长"
	}
	return ""
}

func catalogProducts(g, a *gin.RouterGroup) {
	// 只在空表时写入默认三档：档位可增删改之后，运营把套餐删空再重启不该被塞回来
	var count int64
	if e := db.Model(&Product{}).Count(&count).Error; e != nil {
		panic(e)
	}
	if count == 0 {
		for _, p := range []Product{
			{Days: 7, Price: 990, Enabled: true, Note: "先体验全部会员功能"},
			{Days: 30, Price: 2990, Enabled: true, Note: "覆盖一个完整训练周期"},
			{Days: 365, Price: 12800, Enabled: true, Note: "全年不限次数生成"},
		} {
			if e := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&p).Error; e != nil {
				panic(e)
			}
		}
	}

	// 后台：全部档位（含已下架），带上生效的商品 ID 和「现在能不能买」
	a.GET("/products", func(c *gin.Context) {
		var v []Product
		if e := db.Order("days").Find(&v).Error; e != nil {
			save(c, nil, e)
			return
		}
		for i := range v {
			v[i] = fill(v[i])
		}
		save(c, v, nil)
	})
	a.POST("/products", func(c *gin.Context) {
		if !c.GetBool("admin") {
			fail(c, 403, "需要管理员权限")
			return
		}
		var in struct {
			Days, Price int
			Note        string
			XpayID      string
			Enabled     *bool
		}
		if c.ShouldBindJSON(&in) != nil {
			fail(c, 400, "参数无效")
			return
		}
		in.Note, in.XpayID = strings.TrimSpace(in.Note), strings.TrimSpace(in.XpayID)
		if msg := checkProduct(in.Days, in.Price, in.Note, in.XpayID); msg != "" {
			fail(c, 400, msg)
			return
		}
		enabled := true
		if in.Enabled != nil {
			enabled = *in.Enabled
		}
		p := Product{Days: in.Days, Price: in.Price, Enabled: enabled, Note: in.Note, XpayID: in.XpayID}
		e := db.Transaction(func(tx *gorm.DB) error {
			var dup int64
			if e := tx.Model(&Product{}).Where("days=?", p.Days).Count(&dup).Error; e != nil {
				return e
			}
			if dup > 0 {
				return dupTier
			}
			if e := tx.Create(&p).Error; e != nil {
				return e
			}
			return tx.Create(&Audit{Actor: c.GetString("actor"), Action: "新增套餐 " + strconv.Itoa(p.Days) + " 天", Target: uint(p.Days)}).Error
		})
		result(c, fill(p), e)
	})
	a.PUT("/products/:days", func(c *gin.Context) {
		if !c.GetBool("admin") {
			fail(c, 403, "需要管理员权限")
			return
		}
		old, _ := strconv.Atoi(c.Param("days"))
		// 全部用指针：PUT 允许只改其中几项（上下架就只发 Enabled）
		var in struct {
			Days, Price *int
			Note        *string
			XpayID      *string
			Enabled     *bool
		}
		if c.ShouldBindJSON(&in) != nil {
			fail(c, 400, "参数无效")
			return
		}
		var p Product
		e := db.Transaction(func(tx *gorm.DB) error {
			if e := tx.First(&p, "days=?", old).Error; e != nil {
				return e
			}
			next, changes := p, []string{}
			if in.Days != nil && *in.Days != p.Days {
				next.Days = *in.Days
				changes = append(changes, "天数 "+strconv.Itoa(p.Days)+"→"+strconv.Itoa(next.Days))
			}
			if in.Price != nil && *in.Price != p.Price {
				next.Price = *in.Price
				changes = append(changes, "售价 "+strconv.Itoa(p.Price)+"→"+strconv.Itoa(next.Price))
			}
			if in.Note != nil {
				next.Note = strings.TrimSpace(*in.Note)
				if next.Note != p.Note {
					changes = append(changes, "说明「"+show(p.Note, "未填写")+"」→「"+show(next.Note, "未填写")+"」")
				}
			}
			if in.XpayID != nil {
				next.XpayID = strings.TrimSpace(*in.XpayID)
				if next.XpayID != p.XpayID {
					changes = append(changes, "商品ID「"+show(p.XpayID, "未配置")+"」→「"+show(next.XpayID, "未配置")+"」")
				}
			}
			if in.Enabled != nil && *in.Enabled != p.Enabled {
				next.Enabled = *in.Enabled
				if next.Enabled {
					changes = append(changes, "上架")
				} else {
					changes = append(changes, "下架")
				}
			}
			if msg := checkProduct(next.Days, next.Price, next.Note, next.XpayID); msg != "" {
				return invalid(msg)
			}
			if next.Days != p.Days {
				var dup int64
				if e := tx.Model(&Product{}).Where("days=?", next.Days).Count(&dup).Error; e != nil {
					return e
				}
				if dup > 0 {
					return dupTier
				}
			}
			// days 是主键，改天数等于改主键：显式 Update 一次。用 Save 会按旧主键再插一行
			if e := tx.Model(&Product{}).Where("days=?", p.Days).Updates(map[string]any{
				"days": next.Days, "price": next.Price, "enabled": next.Enabled,
				"note": next.Note, "product": next.XpayID,
			}).Error; e != nil {
				return e
			}
			p = next
			// 什么都没改的 PUT 不写审计：连点两下保存不该多出一条没有信息的记录
			if len(changes) == 0 {
				return nil
			}
			action := "修改套餐 " + strconv.Itoa(p.Days) + " 天：" + strings.Join(changes, "，")
			return tx.Create(&Audit{Actor: c.GetString("actor"), Action: action, Target: uint(p.Days)}).Error
		})
		result(c, fill(p), e)
	})
	a.DELETE("/products/:days", func(c *gin.Context) {
		if !c.GetBool("admin") {
			fail(c, 403, "需要管理员权限")
			return
		}
		days, _ := strconv.Atoi(c.Param("days"))
		e := db.Transaction(func(tx *gorm.DB) error {
			var p Product
			if e := tx.First(&p, "days=?", days).Error; e != nil {
				return e
			}
			// 订单存的是下单时的天数、售价与商品 ID 快照，删档位不影响已付订单的查单与到账
			if e := tx.Delete(&Product{}, "days=?", days).Error; e != nil {
				return e
			}
			return tx.Create(&Audit{Actor: c.GetString("actor"), Action: fmt.Sprintf("删除套餐 %d 天", days), Target: uint(days)}).Error
		})
		save(c, true, e)
	})

	// 小程序：只下发已上架的，Enabled 换成「现在能不能买」——上架但支付没配好就显示暂未开放
	g.GET("/products", func(c *gin.Context) {
		var v []Product
		if e := db.Where("enabled=true").Order("days").Find(&v).Error; e != nil {
			save(c, nil, e)
			return
		}
		for i := range v {
			v[i] = fill(v[i])
			v[i].Enabled = v[i].Ready
		}
		c.JSON(200, v)
	})
}
